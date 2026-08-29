#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: stage-edge-bundle.sh --output <ABS_DIR> --release <p15.x.y|vMAJOR.MINOR.PATCH> --manifest-version <3|4|5> --commit <SHA> --catalog <SHA256> --public-key <HEX> --gh-bin <ABS_FILE> [--node-bin <ABS_FILE> --opencode-bin <ABS_FILE> --opencode-lock <ABS_FILE>] [--codex-bin <ABS_FILE> --codex-pin <ABS_FILE>]\n' >&2
  exit 2
}

OUTPUT=''; RELEASE=''; MANIFEST_VERSION=''; COMMIT=''; CATALOG=''; PUBLIC_KEY=''; NODE_BIN=''; GH_BIN=''; OPENCODE_BIN=''; OPENCODE_LOCK=''; CODEX_BIN=''; CODEX_PIN=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    --manifest-version) MANIFEST_VERSION="${2:-}"; shift 2 ;;
    --commit) COMMIT="${2:-}"; shift 2 ;;
    --catalog) CATALOG="${2:-}"; shift 2 ;;
    --public-key) PUBLIC_KEY="${2:-}"; shift 2 ;;
    --node-bin) NODE_BIN="${2:-}"; shift 2 ;;
    --gh-bin) GH_BIN="${2:-}"; shift 2 ;;
    --opencode-bin) OPENCODE_BIN="${2:-}"; shift 2 ;;
    --opencode-lock) OPENCODE_LOCK="${2:-}"; shift 2 ;;
    --codex-bin) CODEX_BIN="${2:-}"; shift 2 ;;
    --codex-pin) CODEX_PIN="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$OUTPUT" = /* && "$GH_BIN" = /* ]] || usage
[[ "$MANIFEST_VERSION" = 3 || "$MANIFEST_VERSION" = 4 || "$MANIFEST_VERSION" = 5 ]] || usage
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ || "$RELEASE" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || usage
[[ "$COMMIT" =~ ^[a-f0-9]{40}$ ]] || usage
[[ "$CATALOG" =~ ^sha256:[a-f0-9]{64}$ ]] || usage
[[ "$PUBLIC_KEY" =~ ^[a-f0-9]{64}$ ]] || usage
[ -x "$GH_BIN" ] && [ ! -L "$GH_BIN" ] || usage
if [ "$MANIFEST_VERSION" = 3 ] || [ "$MANIFEST_VERSION" = 4 ]; then
  [[ "$NODE_BIN" = /* && "$OPENCODE_BIN" = /* && "$OPENCODE_LOCK" = /* ]] || usage
  [ -x "$NODE_BIN" ] && [ ! -L "$NODE_BIN" ] || usage
  [ -x "$OPENCODE_BIN" ] && [ ! -L "$OPENCODE_BIN" ] || usage
  [ -f "$OPENCODE_LOCK" ] && [ ! -L "$OPENCODE_LOCK" ] || usage
fi
if [ "$MANIFEST_VERSION" = 4 ] || [ "$MANIFEST_VERSION" = 5 ]; then
  [[ "$CODEX_BIN" = /* && "$CODEX_PIN" = /* ]] || usage
  [ -x "$CODEX_BIN" ] && [ ! -L "$CODEX_BIN" ] || usage
  [ -f "$CODEX_PIN" ] && [ ! -L "$CODEX_PIN" ] || usage
fi
[ ! -e "$OUTPUT" ] || { printf 'output already exists\n' >&2; exit 1; }

for command in go install; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing build command: %s\n' "$command" >&2; exit 1; }
done

install -d -m 0755 "$OUTPUT/bin" "$OUTPUT/libexec" "$OUTPUT/systemd"
LDFLAGS="-s -w -X github.com/charle-z/mcp-devbox/internal/buildinfo.Commit=$COMMIT -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleRelease=$RELEASE -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleCatalogHash=$CATALOG -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundlePublicKey=$PUBLIC_KEY"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/bin/mcp-edge" ./cmd/mcp-edge
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/mcp-autopilot-worker" ./cmd/mcp-autopilot-worker
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/mcp-bundle-updater" ./cmd/mcp-bundle-updater

install -m 0755 "$GH_BIN" "$OUTPUT/libexec/gh"
if [ "$MANIFEST_VERSION" = 3 ] || [ "$MANIFEST_VERSION" = 4 ]; then
  install -d -m 0755 "$OUTPUT/opencode" "$OUTPUT/opencode-provider"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/model-turn-driver" ./cmd/model-turn-driver
  install -m 0755 "$OPENCODE_BIN" "$OUTPUT/opencode/opencode"
  install -m 0755 "$NODE_BIN" "$OUTPUT/libexec/node"
  install -m 0644 "$OPENCODE_LOCK" "$OUTPUT/opencode/package-lock.json"
  install -m 0644 integrations/opencode/provider/index.js "$OUTPUT/opencode-provider/index.js"
  install -m 0644 integrations/opencode/provider/htb-actions.js "$OUTPUT/opencode-provider/htb-actions.js"
  install -m 0644 integrations/opencode/provider/dev-actions.js "$OUTPUT/opencode-provider/dev-actions.js"
  install -m 0644 integrations/opencode/provider/package.json "$OUTPUT/opencode-provider/package.json"
fi
if [ "$MANIFEST_VERSION" = 4 ] || [ "$MANIFEST_VERSION" = 5 ]; then
  install -d -m 0755 "$OUTPUT/codex"
  install -m 0755 "$CODEX_BIN" "$OUTPUT/codex/codex"
  install -m 0644 "$CODEX_PIN" "$OUTPUT/codex/pin.json"
fi
if [ "$MANIFEST_VERSION" = 5 ]; then
  install -m 0644 packaging/systemd/mcp-devbox-edge@.service "$OUTPUT/systemd/mcp-devbox-edge@.service"
  install -m 0644 packaging/systemd/mcp-devbox-edge-onboard@.path "$OUTPUT/systemd/mcp-devbox-edge-onboard@.path"
elif [ "$MANIFEST_VERSION" = 4 ]; then
  install -m 0644 packaging/systemd/mcp-devbox-opencode-edge@.service "$OUTPUT/systemd/mcp-devbox-opencode-edge@.service"
else
  install -m 0644 packaging/systemd/mcp-devbox-opencode-edge-bridge@.service "$OUTPUT/systemd/mcp-devbox-opencode-edge@.service"
fi

printf 'staged unsigned Edge bundle %s\n' "$RELEASE"
