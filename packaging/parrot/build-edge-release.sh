#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: build-edge-release.sh --bundle <SIGNED_DIR> --output <DIR> --release p15.x.y --architecture amd64 --channel-tool <ABS_BIN> --private-key <ABS_KEY> --commit <SHA> --protocol <VERSION> --catalog <SHA256>\n' >&2
  exit 2
}

BUNDLE=''; OUTPUT=''; RELEASE=''; ARCHITECTURE=''; CHANNEL_TOOL=''; PRIVATE_KEY=''; COMMIT=''; PROTOCOL=''; CATALOG=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle) BUNDLE="${2:-}"; shift 2 ;;
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    --architecture) ARCHITECTURE="${2:-}"; shift 2 ;;
    --channel-tool) CHANNEL_TOOL="${2:-}"; shift 2 ;;
    --private-key) PRIVATE_KEY="${2:-}"; shift 2 ;;
    --commit) COMMIT="${2:-}"; shift 2 ;;
    --protocol) PROTOCOL="${2:-}"; shift 2 ;;
    --catalog) CATALOG="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done
[[ "$BUNDLE" = /* && "$OUTPUT" = /* && "$CHANNEL_TOOL" = /* && "$PRIVATE_KEY" = /* ]] || usage
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ ]] || usage
[[ "$ARCHITECTURE" = amd64 || "$ARCHITECTURE" = arm64 ]] || usage
: "${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}"

install -d -m 0755 "$OUTPUT/$RELEASE" "$OUTPUT/stable"
ARCHIVE="$OUTPUT/$RELEASE/mcp-devbox-edge_${RELEASE}_${ARCHITECTURE}.tar.gz"
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner \
  --format=posix -C "$BUNDLE" -czf "$ARCHIVE" \
  manifest.json manifest.sig bin/mcp-edge libexec/model-turn-driver \
  libexec/mcp-autopilot-worker libexec/mcp-bundle-updater \
  opencode/opencode opencode/package-lock.json opencode-provider/index.js \
  opencode-provider/htb-actions.js opencode-provider/package.json \
  systemd/mcp-devbox-opencode-edge@.service

"$CHANNEL_TOOL" --archive "$ARCHIVE" --output "$OUTPUT/stable" \
  --release "$RELEASE" --commit "$COMMIT" --protocol "$PROTOCOL" \
  --catalog "$CATALOG" --architecture "$ARCHITECTURE" --private-key "$PRIVATE_KEY"
printf 'built signed update channel for %s\n' "$RELEASE"
