#!/bin/sh
set -eu
umask 077

ROOT_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
PATH=$ROOT_PATH
export PATH
SUPPORTED_OS_IDS="ubuntu debian"
APPARMOR_ENABLED=/sys/module/apparmor/parameters/enabled

fail() {
  echo "mcp-devbox-builder prerequisites: $1" >&2
  exit 1
}

[ "$(id -u)" -eq 0 ] || fail "root is required"
[ "$#" -eq 0 ] || fail "arguments are not accepted"
for tool in apt-get awk cat dpkg-query env grep id; do
  command -v "$tool" >/dev/null 2>&1 || fail "required host tool is missing: $tool"
done

[ -r /etc/os-release ] || fail "host operating system metadata is unavailable"
host_os_id=$(awk -F= '$1 == "ID" {gsub(/^"|"$/, "", $2); print $2; exit}' /etc/os-release)
printf '%s\n' "$host_os_id" | grep -Eq '^[a-z0-9._-]+$' || fail "host operating system identifier is invalid"
case " $SUPPORTED_OS_IDS " in
  *" $host_os_id "*) ;;
  *) fail "unsupported host operating system: $host_os_id" ;;
esac

prerequisites_ready=1
for path in \
  /usr/bin/rootlesskit \
  /usr/bin/newuidmap \
  /usr/bin/newgidmap \
  /usr/bin/slirp4netns \
  /usr/bin/fuse-overlayfs; do
  if [ ! -x "$path" ] || [ -L "$path" ]; then
    prerequisites_ready=0
  fi
done

apparmor_needed=0
if [ -r "$APPARMOR_ENABLED" ] && [ "$(cat "$APPARMOR_ENABLED")" = Y ]; then
  apparmor_needed=1
  if [ ! -x /usr/sbin/apparmor_parser ] || [ -L /usr/sbin/apparmor_parser ]; then
    prerequisites_ready=0
  fi
fi

if [ "$prerequisites_ready" -eq 0 ]; then
  packages="rootlesskit uidmap slirp4netns fuse-overlayfs"
  if [ "$apparmor_needed" -eq 1 ]; then
    packages="$packages apparmor"
  fi
  env -i HOME=/root PATH="$ROOT_PATH" LANG=C LC_ALL=C DEBIAN_FRONTEND=noninteractive apt-get update
  env -i HOME=/root PATH="$ROOT_PATH" LANG=C LC_ALL=C DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $packages
fi

for path in \
  /usr/bin/rootlesskit \
  /usr/bin/newuidmap \
  /usr/bin/newgidmap \
  /usr/bin/slirp4netns \
  /usr/bin/fuse-overlayfs; do
  [ -x "$path" ] && [ ! -L "$path" ] || fail "rootless prerequisite is missing or unsafe: $path"
done

for package in rootlesskit uidmap slirp4netns fuse-overlayfs; do
  version=$(dpkg-query -W -f='${Version}' "$package" 2>/dev/null) || fail "required host package is not installed: $package"
  [ -n "$version" ] || fail "required host package version is empty: $package"
done
if [ "$apparmor_needed" -eq 1 ]; then
  [ -x /usr/sbin/apparmor_parser ] && [ ! -L /usr/sbin/apparmor_parser ] || fail "AppArmor parser is missing or unsafe"
  version=$(dpkg-query -W -f='${Version}' apparmor 2>/dev/null) || fail "required host package is not installed: apparmor"
  [ -n "$version" ] || fail "required host package version is empty: apparmor"
fi

echo "mcp-devbox-builder prerequisites: ready"
