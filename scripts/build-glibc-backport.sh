#!/bin/sh
# Build one coherent libc/loader/development set; do not edit APK metadata.
set -eu
export LC_ALL=C
unset LANGUAGE
policy=/build-input/runtime-remediation.json
source_commit=$(python3 -c 'import json; print(json.load(open("/build-input/runtime-remediation.json"))["glibc_source"])')
src=/tmp/glibc-source
build=/tmp/glibc-build
root=/out/root
control=/out/control
mkdir -p "$src" "$build" "$root" "$control"
curl -fL --retry 2 --max-time 180 "https://gitlab.com/gnutools/glibc/-/archive/$source_commit/glibc-$source_commit.tar.gz" -o /tmp/glibc.tar.gz
printf '%s  %s\n' 7ed4d984e60a8d40cbe4c74b16f9bfba4564f07839f5081dda422252f65b30c0 /tmp/glibc.tar.gz | sha256sum -c -
tar -xzf /tmp/glibc.tar.gz --strip-components=1 -C "$src"
fetch_patch() {
    file=$1 hash=$2 revision=$3
    curl -fL --retry 2 --max-time 60 "https://raw.githubusercontent.com/wolfi-dev/os/$revision/glibc-2.44/$file" -o "/tmp/$file"
    printf '%s  %s\n' "$hash" "/tmp/$file" | sha256sum -c -
    patch --batch --fuzz=0 -d "$src" -p1 < "/tmp/$file"
}
recipe=2b7c39f721e3e6d26b9da7eb5d8cd3032baefbef
fetch_patch 0001-Link-startup-files-without-package-metadata.patch 4fb8772021dcd5f24941b4d031d10b30f81b48d885f71889c669933e5a51d142 "$recipe"
fetch_patch fdo.patch a187de88cb3143c440b33488793ba6dcae5aeb9710d081c587defaba0abb06b4 "$recipe"
fetch_patch fix-allowed-failures-escaping.patch d6a7c6a1affc812ce7f0439645bf4311d26572c73284740baeaa6e0c486bb6f3 "$recipe"
fetch_patch CVE-2026-84243.patch a9d503207f2cabfe2e0b336728202824d466170759950bcc51a8cba152e28e92 "$recipe"
cd "$build"
printf '%s\n' 'slibdir=/usr/lib' 'libdir=/usr/lib' 'rootsbindir=/usr/bin' 'rtlddir=/lib64' \
  'user-defined-trusted-dirs=/usr/local/lib:/usr/local/lib64:/usr/lib:/usr/lib64:/lib:/lib64' > configparms
CPPFLAGS= CFLAGS='-O2' "$src/configure" --prefix=/usr --libdir=/usr/lib --sbindir=/usr/bin \
  --enable-bind-now --enable-fortify-source --enable-stackguard-randomization \
  --enable-stack-protector=strong --enable-cet --with-pkgversion=aeontra-glibc-2.44-r8-CVE-2026-84243 \
  --disable-werror --disable-crypt --enable-kernel=4.9
run_make() {
    phase=$1
    shift
    printf 'glibc_phase=%s\n' "$phase"
    if make -j2 "$@" > "/tmp/glibc-$phase.log" 2>&1; then
        tail -n 3 "/tmp/glibc-$phase.log"
    else
        result=$?
        tail -n 80 "/tmp/glibc-$phase.log"
        exit "$result"
    fi
}
run_make build
run_make abi check-abi
run_make intl subdirs=intl check
grep -q '^PASS: intl/tst-gettext-path-traversal$' intl/tst-gettext-path-traversal.test-result
run_make install install DESTDIR="$root"
# Wolfi's dev package contains real pinned copies as well as the linker scripts.
cp "$root/usr/lib/libc.so.6" "$root/usr/lib/libc.so-2.44"
cp "$root/usr/lib/libm.so.6" "$root/usr/lib/libm.so-2.44"
sed -i 's@/usr/lib/libc\.so\.6@/usr/lib/libc.so-2.44@g' "$root/usr/lib/libc.so"
sed -i 's@/usr/lib/libm\.so\.6@/usr/lib/libm.so-2.44@g; s@/usr/lib/libmvec\.so\.1@/usr/lib/libmvec.so@g' "$root/usr/lib/libm.so"
for so in libanl.so libBrokenLocale.so libc_malloc_debug.so libmvec.so libnss_compat.so libnss_db.so libnss_hesiod.so libresolv.so libthread_db.so; do
    if [ -L "$root/usr/lib/$so" ]; then
        cp --remove-destination "$root/usr/lib/$(readlink "$root/usr/lib/$so")" "$root/usr/lib/$so"
    fi
done
# Keep Wolfi's deliberate charset exclusions.
rm "$root/usr/lib/gconv/IBM1390.so" "$root/usr/lib/gconv/IBM1399.so"
cc -O2 -Wall -Wextra -Werror /build-input/glibc-witness.c -o "$control/glibc-witness"
mkdir -p "$control/locales" "$control/catalog/de_DE/LC_MESSAGES"
I18NPATH="$src/localedata" GCONV_PATH="$root/usr/lib/gconv" \
  "$root/lib64/ld-linux-x86-64.so.2" --library-path "$root/usr/lib" "$root/usr/bin/localedef" \
  --no-archive -i de_DE -f UTF-8 "$control/locales/de_DE.UTF-8"
python3 - "$control/catalog/de_DE/LC_MESSAGES/aeontra-probe.mo" <<'PY'
import struct, sys
original = b'AEONTRA_TRANSLATION_PROBE\0'
translation = b'translated\0'
with open(sys.argv[1], 'wb') as f:
    f.write(struct.pack('<7I', 0x950412de, 0, 1, 28, 36, 0, 0))
    f.write(struct.pack('<4I', len(original)-1, 44, len(translation)-1, 44+len(original)))
    f.write(original+translation)
PY
"$control/glibc-witness" "$control" baseline
"$root/lib64/ld-linux-x86-64.so.2" --library-path "$root/usr/lib" "$control/glibc-witness" "$control" patched
# Wolfi's merged-lib layout owns /lib64 as a symlink into /usr/lib. Keep
# that layout and replace the authoritative loader file, not the symlink.
mv -f "$root/lib64/ld-linux-x86-64.so.2" "$root/usr/lib/ld-linux-x86-64.so.2"
rmdir "$root/lib64"
# Official signed APK used only as a positive/renamed CLI detection control.
cp /usr/bin/sqlite3 "$control/sqlite3-control"
cp "$policy" "$control/runtime-remediation.json"
# Final proof validates every installed file against this build's byte manifest.
python3 - "$root" "$policy" <<'PY'
import hashlib, json, os, pathlib, shutil, sys
root = pathlib.Path(sys.argv[1])
policy = json.load(open(sys.argv[2]))
# Publish only the coherent library/header trees. In particular, do not overlay
# installer-created empty sbin/bin directories on Wolfi's merged-layout symlinks.
def remove(path):
    if path.is_symlink() or not path.is_dir():
        path.unlink()
    else:
        shutil.rmtree(path)
for path in root.iterdir():
    if path.name != 'usr':
        remove(path)
for path in (root/'usr').iterdir():
    if path.name not in ('lib', 'include'):
        remove(path)
manifest = {}
for p in sorted(root.rglob('*')):
    if p.is_file() and not p.is_symlink():
        os.chmod(p, p.stat().st_mode & ~0o6000)
        manifest['/'+str(p.relative_to(root))] = hashlib.sha256(p.read_bytes()).hexdigest()
out = root/'usr/share/aeontra/security'
out.mkdir(parents=True, exist_ok=True)
document = {'schema_version': 1, 'source': policy['glibc_source'], 'patch': policy['glibc_patch'],
            'patch_sha256': policy['glibc_patch_sha256'], 'abi_checked': True, 'intl_checked': True, 'files': manifest}
(out/'glibc-backport.json').write_text(json.dumps(document, sort_keys=True)+'\n')
PY
mkdir -p "$root/usr/share/licenses/aeontra-glibc"
cp "$src/COPYING.LIB" "$src/LICENSES" "$root/usr/share/licenses/aeontra-glibc/"
mkdir -p "$root/usr/share/aeontra/sources/glibc"
cp /tmp/glibc.tar.gz /tmp/0001-Link-startup-files-without-package-metadata.patch /tmp/fdo.patch \
  /tmp/fix-allowed-failures-escaping.patch /tmp/CVE-2026-84243.patch \
  /build-input/build-glibc-backport.sh /build-input/runtime-remediation.json /build-input/glibc-witness.c \
  "$root/usr/share/aeontra/sources/glibc/"
