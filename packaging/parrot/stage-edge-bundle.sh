#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: stage-edge-bundle.sh --output <ABS_DIR> --release p15.x.y --commit <SHA> --catalog <SHA256> --private-key <ABS_FILE> --public-key <HEX> --node-bin <ABS_FILE> --opencode-bin <ABS_FILE> --opencode-lock <ABS_FILE>\n' >&2
  exit 2
}

OUTPUT=''; RELEASE=''; COMMIT=''; CATALOG=''; PRIVATE_KEY=''; PUBLIC_KEY=''; NODE_BIN=''; OPENCODE_BIN=''; OPENCODE_LOCK=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT="${2:-}"; shift 2 ;;
    --release) RELEASE="${2:-}"; shift 2 ;;
    --commit) COMMIT="${2:-}"; shift 2 ;;
    --catalog) CATALOG="${2:-}"; shift 2 ;;
    --private-key) PRIVATE_KEY="${2:-}"; shift 2 ;;
    --public-key) PUBLIC_KEY="${2:-}"; shift 2 ;;
    --node-bin) NODE_BIN="${2:-}"; shift 2 ;;
    --opencode-bin) OPENCODE_BIN="${2:-}"; shift 2 ;;
    --opencode-lock) OPENCODE_LOCK="${2:-}"; shift 2 ;;
    *) usage ;;
  esac
done

[[ "$OUTPUT" = /* && "$PRIVATE_KEY" = /* && "$NODE_BIN" = /* && "$OPENCODE_BIN" = /* && "$OPENCODE_LOCK" = /* ]] || usage
[[ "$RELEASE" =~ ^p15\.[0-9]+\.[0-9]+$ ]] || usage
[[ "$COMMIT" =~ ^[a-f0-9]{40}$ ]] || usage
[[ "$CATALOG" =~ ^sha256:[a-f0-9]{64}$ ]] || usage
[[ "$PUBLIC_KEY" =~ ^[a-f0-9]{64}$ ]] || usage
[ -f "$PRIVATE_KEY" ] && [ ! -L "$PRIVATE_KEY" ] || usage
[ -x "$NODE_BIN" ] && [ ! -L "$NODE_BIN" ] || usage
[ -x "$OPENCODE_BIN" ] && [ ! -L "$OPENCODE_BIN" ] || usage
[ -f "$OPENCODE_LOCK" ] && [ ! -L "$OPENCODE_LOCK" ] || usage
[ ! -e "$OUTPUT" ] || { printf 'output already exists\n' >&2; exit 1; }

for command in go install; do
  command -v "$command" >/dev/null 2>&1 || { printf 'missing build command: %s\n' "$command" >&2; exit 1; }
done

install -d -m 0755 "$OUTPUT/bin" "$OUTPUT/libexec" "$OUTPUT/opencode" "$OUTPUT/opencode-provider" "$OUTPUT/systemd"
LDFLAGS="-s -w -X github.com/charle-z/mcp-devbox/internal/buildinfo.Commit=$COMMIT -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleRelease=$RELEASE -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundleCatalogHash=$CATALOG -X github.com/charle-z/mcp-devbox/internal/buildinfo.EdgeBundlePublicKey=$PUBLIC_KEY"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/bin/mcp-edge" ./cmd/mcp-edge
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/model-turn-driver" ./cmd/model-turn-driver
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/mcp-autopilot-worker" ./cmd/mcp-autopilot-worker
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "$OUTPUT/libexec/mcp-bundle-updater" ./cmd/mcp-bundle-updater

install -m 0755 "$OPENCODE_BIN" "$OUTPUT/opencode/opencode"
install -m 0755 "$NODE_BIN" "$OUTPUT/libexec/node"
install -m 0644 "$OPENCODE_LOCK" "$OUTPUT/opencode/package-lock.json"
install -m 0644 integrations/opencode/provider/index.js "$OUTPUT/opencode-provider/index.js"
install -m 0644 integrations/opencode/provider/htb-actions.js "$OUTPUT/opencode-provider/htb-actions.js"
install -m 0644 integrations/opencode/provider/package.json "$OUTPUT/opencode-provider/package.json"
install -m 0644 packaging/systemd/mcp-devbox-opencode-edge@.service "$OUTPUT/systemd/mcp-devbox-opencode-edge@.service"

go run ./cmd/mcp-bundle-manifest --root "$OUTPUT" --release "$RELEASE" --commit "$COMMIT" \
  --protocol mcp-devbox.edge-bundle.v1 --catalog "$CATALOG" --architecture amd64 --private-key "$PRIVATE_KEY"
printf 'staged signed Edge bundle %s\n' "$RELEASE"
