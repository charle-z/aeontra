#!/bin/sh
set -eu

VERSION=v0.31.2
BASE_URL=https://github.com/moby/buildkit/releases/download/$VERSION
ARCHIVE=buildkit-$VERSION.linux-amd64.tar.gz
SBOM=buildkit-$VERSION.linux-amd64.sbom.json
SIGSTORE=buildkit-$VERSION.linux-amd64.sigstore.json
ARCHIVE_SHA256=fbabdb72433a35f5bb646e4cd424bf8567e5d055710cf55840f7af2020640791
SBOM_SHA256=affbda658a8a8e9ee3bc1d8280ba538d1522adc8a3eb7daaf964904c94628a4f
SIGSTORE_SHA256=b22dca34df188f484547a57758bfc90c658a96cf49c1c09548b435d46d259e90
TARGET=/var/lib/mcp-devbox-builder-staging
WORK=
NEXT=

fail() {
  echo "mcp-devbox-builder stage: $1" >&2
  exit 1
}

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  [ -z "$WORK" ] || rm -rf "$WORK"
  [ -z "$NEXT" ] || rm -rf "$NEXT"
  exit "$status"
}

[ "$(id -u)" -eq 0 ] || fail "root is required"
[ "$#" -eq 0 ] || fail "arguments are not accepted"
for tool in curl sha256sum tar install stat mktemp cmp mv rm mkdir chmod cat; do
  command -v "$tool" >/dev/null 2>&1 || fail "required host tool is missing"
done

WORK=$(mktemp -d /var/lib/mcp-devbox-builder-download.XXXXXX)
NEXT=$(mktemp -d /var/lib/mcp-devbox-builder-staging.XXXXXX)
chmod 0700 "$WORK" "$NEXT"
trap cleanup EXIT HUP INT TERM

fetch() {
  name=$1
  curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error \
    --retry 3 --retry-all-errors --connect-timeout 15 --max-time 300 \
    --output "$WORK/$name" "$BASE_URL/$name"
  [ -f "$WORK/$name" ] && [ ! -L "$WORK/$name" ] || fail "downloaded asset is unsafe"
}

fetch "$ARCHIVE"
fetch "$SBOM"
fetch "$SIGSTORE"
printf '%s  %s\n' "$ARCHIVE_SHA256" "$WORK/$ARCHIVE" | sha256sum --check --strict >/dev/null || fail "archive digest mismatch"
printf '%s  %s\n' "$SBOM_SHA256" "$WORK/$SBOM" | sha256sum --check --strict >/dev/null || fail "SBOM digest mismatch"
printf '%s  %s\n' "$SIGSTORE_SHA256" "$WORK/$SIGSTORE" | sha256sum --check --strict >/dev/null || fail "Sigstore bundle digest mismatch"

tar --extract --gzip --file "$WORK/$ARCHIVE" --directory "$WORK" \
  --no-same-owner --no-same-permissions \
  bin/buildkitd bin/buildctl bin/buildkit-runc
for name in buildkitd buildctl buildkit-runc; do
  source=$WORK/bin/$name
  [ -f "$source" ] && [ ! -L "$source" ] || fail "release member is missing or unsafe"
  install -o root -g root -m 0755 "$source" "$NEXT/$name"
done
mkdir -m 0700 "$NEXT/evidence"
install -o root -g root -m 0600 "$WORK/$SBOM" "$NEXT/evidence/$SBOM"
install -o root -g root -m 0600 "$WORK/$SIGSTORE" "$NEXT/evidence/$SIGSTORE"
printf '%s\n' "$VERSION" > "$NEXT/VERSION"
printf '%s\n' "$BASE_URL/$ARCHIVE" > "$NEXT/SOURCE"
printf '%s  %s\n' "$ARCHIVE_SHA256" "$ARCHIVE" > "$NEXT/RELEASE_SHA256"
chmod 0600 "$NEXT/VERSION" "$NEXT/SOURCE" "$NEXT/RELEASE_SHA256"
(
  cd "$NEXT"
  sha256sum buildkitd buildctl buildkit-runc > SHA256SUMS
)
chmod 0600 "$NEXT/SHA256SUMS"

if [ -e "$TARGET" ] || [ -L "$TARGET" ]; then
  [ -d "$TARGET" ] && [ ! -L "$TARGET" ] || fail "existing staging target is unsafe"
  set -- $(stat -c '%u %a' "$TARGET")
  [ "$1" = 0 ] && [ $((0$2 & 0077)) -eq 0 ] || fail "existing staging target is not private root-owned state"
  [ -f "$TARGET/VERSION" ] && [ "$(cat "$TARGET/VERSION")" = "$VERSION" ] || fail "different staging version already exists"
  cmp -s "$TARGET/SHA256SUMS" "$NEXT/SHA256SUMS" || fail "existing staging content differs"
  rm -rf "$NEXT"
  NEXT=
  echo "mcp-devbox-builder stage: existing $VERSION staging verified"
  exit 0
fi

mv "$NEXT" "$TARGET"
NEXT=
echo "mcp-devbox-builder stage: prepared $VERSION at $TARGET"
