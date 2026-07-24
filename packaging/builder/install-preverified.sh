#!/bin/sh
set -eu

STAGING=/var/lib/mcp-devbox-builder-staging
INSTALL_ROOT=/usr/local/lib/mcp-devbox-builder
CONFIG_ROOT=/etc/mcp-devbox-builder
UNIT_DIR=/etc/systemd/system
UNIT_NAME=mcp-devbox-buildkit.service
SCRIPT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
UNIT_SOURCE=$SCRIPT_ROOT/$UNIT_NAME
CONFIG_SOURCE=$SCRIPT_ROOT/buildkitd.toml
SOCKET=/run/mcp-devbox-buildkit/buildkit/buildkitd.sock
ROLLBACK=
WAS_ACTIVE=0

fail() {
  echo "mcp-devbox-builder install: $1" >&2
  exit 1
}

is_root_owned_private_file() {
  path=$1
  [ -f "$path" ] || return 1
  [ ! -L "$path" ] || return 1
  set -- $(stat -c '%u %a' "$path")
  [ "$1" = 0 ] || return 1
  mode=$2
  [ $((0$mode & 0022)) -eq 0 ] || return 1
}

restore_file() {
  target=$1
  name=$2
  if [ -f "$ROLLBACK/$name" ]; then
    install -o root -g root -m 0755 "$ROLLBACK/$name" "$target"
  else
    rm -f "$target"
  fi
}

rollback() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ -n "$ROLLBACK" ] && [ -d "$ROLLBACK" ]; then
    systemctl stop "$UNIT_NAME" >/dev/null 2>&1 || true
    restore_file "$INSTALL_ROOT/buildkitd" buildkitd
    restore_file "$INSTALL_ROOT/buildctl" buildctl
    if [ -f "$ROLLBACK/buildkitd.toml" ]; then
      install -o root -g root -m 0644 "$ROLLBACK/buildkitd.toml" "$CONFIG_ROOT/buildkitd.toml"
    else
      rm -f "$CONFIG_ROOT/buildkitd.toml"
    fi
    if [ -f "$ROLLBACK/$UNIT_NAME" ]; then
      install -o root -g root -m 0644 "$ROLLBACK/$UNIT_NAME" "$UNIT_DIR/$UNIT_NAME"
    else
      rm -f "$UNIT_DIR/$UNIT_NAME"
    fi
    systemctl daemon-reload >/dev/null 2>&1 || true
    if [ "$WAS_ACTIVE" -eq 1 ]; then
      systemctl enable --now "$UNIT_NAME" >/dev/null 2>&1 || true
    fi
    rm -rf "$ROLLBACK"
  fi
  exit "$status"
}

[ "$(id -u)" -eq 0 ] || fail "root is required"
[ "$#" -eq 0 ] || fail "arguments are not accepted"
for tool in stat sha256sum install systemctl useradd getent runuser; do
  command -v "$tool" >/dev/null 2>&1 || fail "required host tool is missing"
done
for tool in /usr/bin/rootlesskit /usr/bin/newuidmap /usr/bin/newgidmap /usr/bin/slirp4netns; do
  [ -x "$tool" ] && [ ! -L "$tool" ] || fail "rootless prerequisite is missing or unsafe"
done
[ -d "$STAGING" ] && [ ! -L "$STAGING" ] || fail "preverified staging directory is missing"
set -- $(stat -c '%u %a' "$STAGING")
[ "$1" = 0 ] && [ $((0$2 & 0077)) -eq 0 ] || fail "preverified staging directory is not private root-owned state"
for file in buildkitd buildctl SHA256SUMS; do
  is_root_owned_private_file "$STAGING/$file" || fail "preverified staging file is unsafe"
done
is_root_owned_private_file "$UNIT_SOURCE" || fail "service source is unsafe"
is_root_owned_private_file "$CONFIG_SOURCE" || fail "configuration source is unsafe"
[ "$(wc -l < "$STAGING/SHA256SUMS" | tr -d ' ')" -eq 2 ] || fail "checksum manifest must contain exactly two entries"
grep -Eq '^[a-f0-9]{64}  buildkitd$' "$STAGING/SHA256SUMS" || fail "buildkitd checksum entry is invalid"
grep -Eq '^[a-f0-9]{64}  buildctl$' "$STAGING/SHA256SUMS" || fail "buildctl checksum entry is invalid"
(
  cd "$STAGING"
  sha256sum --check --strict SHA256SUMS >/dev/null
) || fail "preverified staging checksum failed"

if getent passwd mcp-build >/dev/null 2>&1; then
  set -- $(getent passwd mcp-build | tr ':' ' ')
  [ "$3" -ne 0 ] || fail "builder account must not be root"
  [ "$6" = /var/lib/mcp-devbox-buildkit ] || fail "builder account home is invalid"
  [ "$7" = /usr/sbin/nologin ] || fail "builder account shell is invalid"
else
  useradd --system --user-group --create-home --add-subids-for-system \
    --home-dir /var/lib/mcp-devbox-buildkit --shell /usr/sbin/nologin mcp-build
fi
grep -Eq '^mcp-build:[0-9]+:[0-9]+$' /etc/subuid || fail "builder subuid allocation is missing"
grep -Eq '^mcp-build:[0-9]+:[0-9]+$' /etc/subgid || fail "builder subgid allocation is missing"

install -d -o root -g root -m 0755 "$INSTALL_ROOT" "$CONFIG_ROOT"
ROLLBACK=$(mktemp -d /var/lib/mcp-devbox-builder-rollback.XXXXXX)
chmod 0700 "$ROLLBACK"
for name in buildkitd buildctl; do
  if [ -f "$INSTALL_ROOT/$name" ] && [ ! -L "$INSTALL_ROOT/$name" ]; then
    cp -p "$INSTALL_ROOT/$name" "$ROLLBACK/$name"
  fi
done
if [ -f "$CONFIG_ROOT/buildkitd.toml" ] && [ ! -L "$CONFIG_ROOT/buildkitd.toml" ]; then
  cp -p "$CONFIG_ROOT/buildkitd.toml" "$ROLLBACK/buildkitd.toml"
fi
if [ -f "$UNIT_DIR/$UNIT_NAME" ] && [ ! -L "$UNIT_DIR/$UNIT_NAME" ]; then
  cp -p "$UNIT_DIR/$UNIT_NAME" "$ROLLBACK/$UNIT_NAME"
fi
if systemctl is-active --quiet "$UNIT_NAME"; then
  WAS_ACTIVE=1
fi
trap rollback EXIT HUP INT TERM

install -o root -g root -m 0755 "$STAGING/buildkitd" "$INSTALL_ROOT/.buildkitd.new"
install -o root -g root -m 0755 "$STAGING/buildctl" "$INSTALL_ROOT/.buildctl.new"
mv -f "$INSTALL_ROOT/.buildkitd.new" "$INSTALL_ROOT/buildkitd"
mv -f "$INSTALL_ROOT/.buildctl.new" "$INSTALL_ROOT/buildctl"
install -o root -g root -m 0644 "$CONFIG_SOURCE" "$CONFIG_ROOT/buildkitd.toml"
install -o root -g root -m 0644 "$UNIT_SOURCE" "$UNIT_DIR/$UNIT_NAME"
systemctl daemon-reload
systemctl enable --now "$UNIT_NAME"

ready=0
for _ in $(seq 1 30); do
  if systemctl is-active --quiet "$UNIT_NAME" && [ -S "$SOCKET" ]; then
    if runuser -u mcp-build -- "$INSTALL_ROOT/buildctl" --addr "unix://$SOCKET" debug workers >/dev/null 2>&1; then
      ready=1
      break
    fi
  fi
  sleep 1
done
[ "$ready" -eq 1 ] || fail "rootless BuildKit did not become healthy"

trap - EXIT HUP INT TERM
rm -rf "$ROLLBACK"
ROLLBACK=
echo "mcp-devbox-builder install: healthy private candidate"
