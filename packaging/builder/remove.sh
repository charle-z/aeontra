#!/bin/sh
set -eu

UNIT_NAME=mcp-devbox-buildkit.service
UNIT=/etc/systemd/system/$UNIT_NAME
INSTALL_ROOT=/usr/local/lib/mcp-devbox-builder
CONFIG_ROOT=/etc/mcp-devbox-builder

[ "$(id -u)" -eq 0 ] || {
  echo "mcp-devbox-builder remove: root is required" >&2
  exit 1
}
[ "$#" -eq 0 ] || {
  echo "mcp-devbox-builder remove: arguments are not accepted" >&2
  exit 1
}

systemctl disable --now "$UNIT_NAME" >/dev/null 2>&1 || true
for path in "$UNIT" "$CONFIG_ROOT/buildkitd.toml" "$INSTALL_ROOT/buildkitd" "$INSTALL_ROOT/buildctl"; do
  if [ -L "$path" ]; then
    echo "mcp-devbox-builder remove: refusing symlinked managed path" >&2
    exit 1
  fi
done
rm -f "$UNIT" "$CONFIG_ROOT/buildkitd.toml" "$INSTALL_ROOT/buildkitd" "$INSTALL_ROOT/buildctl"
rmdir "$CONFIG_ROOT" "$INSTALL_ROOT" 2>/dev/null || true
systemctl daemon-reload

echo "mcp-devbox-builder remove: binaries, configuration and unit removed"
echo "mcp-devbox-builder remove: state, cache, user and preverified staging were preserved"
