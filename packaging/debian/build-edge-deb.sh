#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: build-edge-deb.sh --bundle <SIGNED_RELEASE_DIR> --output <DIR> --release <p15.x.y|vMAJOR.MINOR.PATCH> --signing-key <GPG_KEY_ID>\n' >&2
  exit 2
}

BUNDLE=""
OUTPUT=""
RELEASE=""
SIGNING_KEY=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) BUNDLE="${2:-}"; shift 2 ;;
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    --signing-key) SIGNING_KEY="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$BUNDLE" = /* && "$OUTPUT" = /* ]] || usage
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ || "$RELEASE" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || usage
[ -n "$SIGNING_KEY" ] || usage
PACKAGE_VERSION="${RELEASE#p}"
if [[ "$RELEASE" = v* ]]; then
  PACKAGE_VERSION="${RELEASE#v}"
fi
for command in dpkg-deb gpg sha256sum install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing build command: %s\n' "$command" >&2; exit 1; }
done

for path in \
  manifest.json manifest.sig bin/mcp-edge libexec/gh \
  libexec/mcp-autopilot-worker libexec/mcp-bundle-updater; do
  [ -f "$BUNDLE/$path" ] && [ ! -L "$BUNDLE/$path" ] || {
    printf 'signed bundle is incomplete: %s\n' "$path" >&2
    exit 1
  }
done
HAS_OPENCODE=0
if [ -f "$BUNDLE/opencode/opencode" ] || [ -f "$BUNDLE/opencode/package-lock.json" ]; then
  for path in libexec/model-turn-driver libexec/node opencode/opencode opencode/package-lock.json \
    opencode-provider/index.js opencode-provider/htb-actions.js opencode-provider/dev-actions.js \
    opencode-provider/package.json; do
    [ -f "$BUNDLE/$path" ] && [ ! -L "$BUNDLE/$path" ] || {
      printf 'signed OpenCode components are incomplete: %s\n' "$path" >&2
      exit 1
    }
  done
  HAS_OPENCODE=1
fi
HAS_CODEX=0
if [ -f "$BUNDLE/codex/codex" ] || [ -f "$BUNDLE/codex/pin.json" ]; then
  [ -f "$BUNDLE/codex/codex" ] && [ ! -L "$BUNDLE/codex/codex" ] && [ -f "$BUNDLE/codex/pin.json" ] && [ ! -L "$BUNDLE/codex/pin.json" ] || {
    printf 'signed Codex components are incomplete\n' >&2
    exit 1
  }
  HAS_CODEX=1
fi
EDGE_UNIT=''
if [ -f "$BUNDLE/systemd/mcp-devbox-edge@.service" ] && [ ! -e "$BUNDLE/systemd/mcp-devbox-opencode-edge@.service" ]; then
  EDGE_UNIT='mcp-devbox-edge@.service'
elif [ -f "$BUNDLE/systemd/mcp-devbox-opencode-edge@.service" ] && [ ! -e "$BUNDLE/systemd/mcp-devbox-edge@.service" ]; then
  EDGE_UNIT='mcp-devbox-opencode-edge@.service'
else
  printf 'signed Edge unit selection is invalid\n' >&2
  exit 1
fi

SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required for reproducible package content}"
export SOURCE_DATE_EPOCH
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
PACKAGE_ROOT="$STAGING/mcp-devbox-edge"
RELEASE_ROOT="$PACKAGE_ROOT/opt/mcp-devbox/releases/$RELEASE"

install -d -m 0755 \
  "$PACKAGE_ROOT/DEBIAN" "$RELEASE_ROOT/bin" "$RELEASE_ROOT/libexec" \
  "$RELEASE_ROOT/opencode" "$RELEASE_ROOT/opencode-provider" "$RELEASE_ROOT/codex" "$RELEASE_ROOT/systemd" \
  "$PACKAGE_ROOT/etc/mcp-devbox" "$PACKAGE_ROOT/usr/local/bin" \
  "$PACKAGE_ROOT/usr/local/libexec/mcp-devbox" "$PACKAGE_ROOT/usr/share/doc/mcp-devbox" \
  "$PACKAGE_ROOT/etc/systemd/system"

install -d -m 0755 "$PACKAGE_ROOT/usr/share/mcp-devbox" "$PACKAGE_ROOT/etc/polkit-1/rules.d"

install -m 0755 "$BUNDLE/bin/mcp-edge" "$RELEASE_ROOT/bin/mcp-edge"
install -m 0755 "$BUNDLE/libexec/mcp-autopilot-worker" "$RELEASE_ROOT/libexec/mcp-autopilot-worker"
install -m 0755 "$BUNDLE/libexec/mcp-bundle-updater" "$RELEASE_ROOT/libexec/mcp-bundle-updater"
install -m 0755 "$BUNDLE/libexec/gh" "$RELEASE_ROOT/libexec/gh"
if [ "$HAS_OPENCODE" -eq 1 ]; then
  install -m 0755 "$BUNDLE/libexec/model-turn-driver" "$RELEASE_ROOT/libexec/model-turn-driver"
  install -m 0755 "$BUNDLE/libexec/node" "$RELEASE_ROOT/libexec/node"
  install -m 0755 "$BUNDLE/opencode/opencode" "$RELEASE_ROOT/opencode/opencode"
  install -m 0644 "$BUNDLE/opencode/package-lock.json" "$RELEASE_ROOT/opencode/package-lock.json"
  install -m 0644 "$BUNDLE/opencode-provider/index.js" "$RELEASE_ROOT/opencode-provider/index.js"
  install -m 0644 "$BUNDLE/opencode-provider/htb-actions.js" "$RELEASE_ROOT/opencode-provider/htb-actions.js"
  install -m 0644 "$BUNDLE/opencode-provider/dev-actions.js" "$RELEASE_ROOT/opencode-provider/dev-actions.js"
  install -m 0644 "$BUNDLE/opencode-provider/package.json" "$RELEASE_ROOT/opencode-provider/package.json"
fi
if [ "$HAS_CODEX" -eq 1 ]; then
  install -m 0755 "$BUNDLE/codex/codex" "$RELEASE_ROOT/codex/codex"
  install -m 0644 "$BUNDLE/codex/pin.json" "$RELEASE_ROOT/codex/pin.json"
fi
install -m 0644 "$BUNDLE/systemd/$EDGE_UNIT" "$RELEASE_ROOT/systemd/$EDGE_UNIT"
install -m 0644 "$BUNDLE/manifest.json" "$RELEASE_ROOT/manifest.json"
install -m 0644 "$BUNDLE/manifest.sig" "$RELEASE_ROOT/manifest.sig"
install -m 0755 packaging/parrot/onboarding-preflight.sh "$PACKAGE_ROOT/usr/local/libexec/mcp-devbox/onboarding-preflight"
install -m 0644 packaging/systemd/mcp-devbox-bundle-updater.service "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-bundle-updater.service"
install -m 0644 packaging/systemd/mcp-devbox-bundle-rollback.service "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-bundle-rollback.service"
install -m 0644 packaging/systemd/mcp-devbox-edge-repair.service "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-edge-repair.service"
install -m 0644 packaging/polkit/49-mcp-devbox-updater.rules.in "$PACKAGE_ROOT/usr/share/mcp-devbox/49-mcp-devbox-updater.rules.in"
if [ "$EDGE_UNIT" = 'mcp-devbox-edge@.service' ]; then
  install -m 0644 "$BUNDLE/systemd/mcp-devbox-edge-onboard@.path" "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-edge-onboard@.path"
else
  sed 's/Unit=mcp-devbox-edge@%i.service/Unit=mcp-devbox-opencode-edge@%i.service/' packaging/systemd/mcp-devbox-edge-onboard@.path >"$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-edge-onboard@.path"
  chmod 0644 "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-edge-onboard@.path"
fi
install -m 0644 packaging/parrot/autopilot-model.json "$PACKAGE_ROOT/etc/mcp-devbox/autopilot-model.json"
install -m 0644 docs/edge-bundles.md "$PACKAGE_ROOT/usr/share/doc/mcp-devbox/edge-bundles.md"

printf '%s\n' '/etc/mcp-devbox/autopilot-model.json' >"$PACKAGE_ROOT/DEBIAN/conffiles"

sed "s/@RELEASE@/$RELEASE/g" packaging/debian/postinst.in >"$PACKAGE_ROOT/DEBIAN/postinst"
chmod 0755 "$PACKAGE_ROOT/DEBIAN/postinst"
install -m 0755 packaging/debian/prerm "$PACKAGE_ROOT/DEBIAN/prerm"

cat >"$PACKAGE_ROOT/DEBIAN/control" <<EOF
Package: mcp-devbox-edge
Version: $PACKAGE_VERSION
Architecture: amd64
Maintainer: MCP Devbox Release Engineering
Depends: bubblewrap, catatonit, chromium, curl, git, golang-go, podman, policykit-1 | polkitd, python3, systemd, util-linux
Section: devel
Priority: optional
Description: Signed MCP Devbox Edge and local autopilot bundle
EOF

find "$PACKAGE_ROOT" -print0 | xargs -0 touch --no-dereference --date="@$SOURCE_DATE_EPOCH"
install -d -m 0755 "$OUTPUT"
DEB="$OUTPUT/mcp-devbox-edge_${PACKAGE_VERSION}_amd64.deb"
dpkg-deb --root-owner-group --build "$PACKAGE_ROOT" "$DEB"
gpg --batch --yes --local-user "$SIGNING_KEY" --armor --detach-sign --output "$DEB.asc" "$DEB"
(cd "$OUTPUT" && sha256sum "$(basename "$DEB")") >"$DEB.sha256"
printf 'built signed package %s\n' "$DEB"
