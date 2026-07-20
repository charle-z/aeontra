#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: build-edge-deb.sh --bundle <SIGNED_RELEASE_DIR> --output <DIR> --release p15.x.y --signing-key <GPG_KEY_ID>\n' >&2
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
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ ]] || usage
[ -n "$SIGNING_KEY" ] || usage
for command in dpkg-deb gpg sha256sum install mktemp; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing build command: %s\n' "$command" >&2; exit 1; }
done

for path in \
  manifest.json manifest.sig bin/mcp-edge libexec/model-turn-driver \
  libexec/mcp-autopilot-worker libexec/mcp-bundle-updater opencode/opencode opencode/package-lock.json \
  opencode-provider/index.js opencode-provider/htb-actions.js \
  opencode-provider/package.json systemd/mcp-devbox-opencode-edge@.service; do
  [ -f "$BUNDLE/$path" ] && [ ! -L "$BUNDLE/$path" ] || {
    printf 'signed bundle is incomplete: %s\n' "$path" >&2
    exit 1
  }
done

SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required for reproducible package content}"
export SOURCE_DATE_EPOCH
STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT
PACKAGE_ROOT="$STAGING/mcp-devbox-edge"
RELEASE_ROOT="$PACKAGE_ROOT/opt/mcp-devbox/releases/$RELEASE"

install -d -m 0755 \
  "$PACKAGE_ROOT/DEBIAN" "$RELEASE_ROOT/bin" "$RELEASE_ROOT/libexec" \
  "$RELEASE_ROOT/opencode" "$RELEASE_ROOT/opencode-provider" "$RELEASE_ROOT/systemd" \
  "$PACKAGE_ROOT/etc/mcp-devbox" "$PACKAGE_ROOT/usr/local/bin" \
  "$PACKAGE_ROOT/usr/local/libexec/mcp-devbox" "$PACKAGE_ROOT/usr/share/doc/mcp-devbox" \
  "$PACKAGE_ROOT/etc/systemd/system"

install -m 0755 "$BUNDLE/bin/mcp-edge" "$RELEASE_ROOT/bin/mcp-edge"
install -m 0755 "$BUNDLE/libexec/model-turn-driver" "$RELEASE_ROOT/libexec/model-turn-driver"
install -m 0755 "$BUNDLE/libexec/mcp-autopilot-worker" "$RELEASE_ROOT/libexec/mcp-autopilot-worker"
install -m 0755 "$BUNDLE/libexec/mcp-bundle-updater" "$RELEASE_ROOT/libexec/mcp-bundle-updater"
install -m 0755 "$BUNDLE/opencode/opencode" "$RELEASE_ROOT/opencode/opencode"
install -m 0644 "$BUNDLE/opencode/package-lock.json" "$RELEASE_ROOT/opencode/package-lock.json"
install -m 0644 "$BUNDLE/opencode-provider/index.js" "$RELEASE_ROOT/opencode-provider/index.js"
install -m 0644 "$BUNDLE/opencode-provider/htb-actions.js" "$RELEASE_ROOT/opencode-provider/htb-actions.js"
install -m 0644 "$BUNDLE/opencode-provider/package.json" "$RELEASE_ROOT/opencode-provider/package.json"
install -m 0644 "$BUNDLE/systemd/mcp-devbox-opencode-edge@.service" "$RELEASE_ROOT/systemd/mcp-devbox-opencode-edge@.service"
install -m 0644 "$BUNDLE/manifest.json" "$RELEASE_ROOT/manifest.json"
install -m 0644 "$BUNDLE/manifest.sig" "$RELEASE_ROOT/manifest.sig"
install -m 0755 packaging/parrot/onboarding-preflight.sh "$PACKAGE_ROOT/usr/local/libexec/mcp-devbox/onboarding-preflight"
install -m 0644 packaging/systemd/mcp-devbox-bundle-updater.service "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-bundle-updater.service"
install -m 0644 packaging/systemd/mcp-devbox-edge-onboard@.path "$PACKAGE_ROOT/etc/systemd/system/mcp-devbox-edge-onboard@.path"
install -m 0644 docs/edge-bundles.md "$PACKAGE_ROOT/usr/share/doc/mcp-devbox/edge-bundles.md"

sed "s/@RELEASE@/$RELEASE/g" packaging/debian/postinst.in >"$PACKAGE_ROOT/DEBIAN/postinst"
chmod 0755 "$PACKAGE_ROOT/DEBIAN/postinst"
install -m 0755 packaging/debian/prerm "$PACKAGE_ROOT/DEBIAN/prerm"

cat >"$PACKAGE_ROOT/DEBIAN/control" <<EOF
Package: mcp-devbox-edge
Version: ${RELEASE#p}
Architecture: amd64
Maintainer: MCP Devbox Release Engineering
Depends: bubblewrap, curl, git, nodejs, npm, python3, systemd
Section: devel
Priority: optional
Description: Signed MCP Devbox Edge and local autopilot bundle
EOF

find "$PACKAGE_ROOT" -print0 | xargs -0 touch --no-dereference --date="@$SOURCE_DATE_EPOCH"
install -d -m 0755 "$OUTPUT"
DEB="$OUTPUT/mcp-devbox-edge_${RELEASE#p}_amd64.deb"
dpkg-deb --root-owner-group --build "$PACKAGE_ROOT" "$DEB"
gpg --batch --yes --local-user "$SIGNING_KEY" --armor --detach-sign --output "$DEB.asc" "$DEB"
sha256sum "$DEB" >"$DEB.sha256"
printf 'built signed package %s\n' "$DEB"
