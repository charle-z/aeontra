#!/bin/sh
set -eu

UNIT_NAME=mcp-devbox-buildkit.service
UNIT=/etc/systemd/system/$UNIT_NAME
INSTALL_ROOT=/usr/local/lib/mcp-devbox-builder
CONFIG_ROOT=/etc/mcp-devbox-builder
APPARMOR_PROFILE=/etc/apparmor.d/mcp-devbox-buildkit-runc
APPARMOR_ENABLED=/sys/module/apparmor/parameters/enabled
APPARMOR_PARSER=/usr/sbin/apparmor_parser

[ "$(id -u)" -eq 0 ] || {
  echo "mcp-devbox-builder remove: root is required" >&2
  exit 1
}
[ "$#" -eq 0 ] || {
  echo "mcp-devbox-builder remove: arguments are not accepted" >&2
  exit 1
}

systemctl disable --now "$UNIT_NAME" >/dev/null 2>&1 || true
for path in "$UNIT" "$CONFIG_ROOT/buildkitd.toml" "$APPARMOR_PROFILE" "$INSTALL_ROOT/buildkitd" "$INSTALL_ROOT/buildctl" "$INSTALL_ROOT/buildkit-runc"; do
  if [ -L "$path" ]; then
    echo "mcp-devbox-builder remove: refusing symlinked managed path" >&2
    exit 1
  fi
done
if [ -r "$APPARMOR_ENABLED" ] && [ "$(cat "$APPARMOR_ENABLED")" = Y ] && [ -f "$APPARMOR_PROFILE" ]; then
  [ -x "$APPARMOR_PARSER" ] && [ ! -L "$APPARMOR_PARSER" ] || {
    echo "mcp-devbox-builder remove: AppArmor parser is missing or unsafe" >&2
    exit 1
  }
  "$APPARMOR_PARSER" -R "$APPARMOR_PROFILE" >/dev/null 2>&1 || true
fi
rm -f "$UNIT" "$CONFIG_ROOT/buildkitd.toml" "$APPARMOR_PROFILE" "$INSTALL_ROOT/buildkitd" "$INSTALL_ROOT/buildctl" "$INSTALL_ROOT/buildkit-runc"
rmdir "$CONFIG_ROOT" "$INSTALL_ROOT" 2>/dev/null || true
systemctl daemon-reload

echo "mcp-devbox-builder remove: binaries, configuration, AppArmor profile and unit removed"
echo "mcp-devbox-builder remove: state, cache, user and preverified staging were preserved"
