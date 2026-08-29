#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: build-edge-release.sh --bundle <SIGNED_DIR> --output <DIR> --release <p15.x.y|vMAJOR.MINOR.PATCH> --architecture <amd64|arm64>\n' >&2
  exit 2
}

BUNDLE=''; OUTPUT=''; RELEASE=''; ARCHITECTURE=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) BUNDLE="${2:-}"; shift 2 ;;
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    --architecture) ARCHITECTURE="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done
[[ "$BUNDLE" = /* && "$OUTPUT" = /* ]] || usage
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ || "$RELEASE" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || usage
[[ "$ARCHITECTURE" = amd64 || "$ARCHITECTURE" = arm64 ]] || usage
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"

install -d -m 0755 "$OUTPUT/$RELEASE" "$OUTPUT/stable"
ARCHIVE="$OUTPUT/$RELEASE/mcp-devbox-edge_${RELEASE}_${ARCHITECTURE}.tar.gz"
COMPONENTS=(manifest.json manifest.sig bin/mcp-edge libexec/gh libexec/mcp-autopilot-worker libexec/mcp-bundle-updater)
if [ -f "$BUNDLE/codex/codex" ] || [ -f "$BUNDLE/codex/pin.json" ]; then
  [ -f "$BUNDLE/codex/codex" ] && [ ! -L "$BUNDLE/codex/codex" ] && [ -f "$BUNDLE/codex/pin.json" ] && [ ! -L "$BUNDLE/codex/pin.json" ] || {
    printf 'signed Codex components are incomplete\n' >&2
    exit 1
  }
  COMPONENTS+=(codex/codex codex/pin.json)
fi
if [ -f "$BUNDLE/opencode/opencode" ] || [ -f "$BUNDLE/opencode/package-lock.json" ]; then
  for path in libexec/model-turn-driver libexec/node opencode/opencode opencode/package-lock.json opencode-provider/index.js opencode-provider/htb-actions.js opencode-provider/dev-actions.js opencode-provider/package.json; do
    [ -f "$BUNDLE/$path" ] && [ ! -L "$BUNDLE/$path" ] || { printf 'signed OpenCode components are incomplete: %s\n' "$path" >&2; exit 1; }
    COMPONENTS+=("$path")
  done
fi
if [ -f "$BUNDLE/systemd/mcp-devbox-edge@.service" ] && [ ! -e "$BUNDLE/systemd/mcp-devbox-opencode-edge@.service" ]; then
  [ -f "$BUNDLE/systemd/mcp-devbox-edge-onboard@.path" ] && [ ! -L "$BUNDLE/systemd/mcp-devbox-edge-onboard@.path" ] || { printf 'signed Edge onboarding path is unavailable\n' >&2; exit 1; }
  COMPONENTS+=(systemd/mcp-devbox-edge@.service)
  COMPONENTS+=(systemd/mcp-devbox-edge-onboard@.path)
elif [ -f "$BUNDLE/systemd/mcp-devbox-opencode-edge@.service" ] && [ ! -e "$BUNDLE/systemd/mcp-devbox-edge@.service" ]; then
  COMPONENTS+=(systemd/mcp-devbox-opencode-edge@.service)
else
  printf 'signed Edge unit selection is invalid\n' >&2
  exit 1
fi
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
  --format=posix -C "$BUNDLE" -czf "$ARCHIVE" \
  "${COMPONENTS[@]}"
(cd "$(dirname "$ARCHIVE")" && sha256sum "$(basename "$ARCHIVE")") >"$ARCHIVE.sha256"

printf 'built deterministic Edge archive for %s\n' "$RELEASE"
