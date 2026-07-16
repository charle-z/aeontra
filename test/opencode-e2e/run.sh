#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
image=${OPENCODE_E2E_IMAGE:-mcp-devbox-opencode-e2e:local}
output=${OPENCODE_E2E_OUTPUT:-"$repo_root/artifacts"}
container="mcp-devbox-opencode-e2e-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-0}"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$output"
chmod 0777 "$output"

timeout --signal=TERM --kill-after=10s 12m \
  docker build --no-cache --pull \
    --file "$repo_root/test/opencode-e2e/Dockerfile" \
    --tag "$image" \
    "$repo_root"

timeout --signal=TERM --kill-after=10s 15m \
docker run --rm \
  --name "$container" \
  --network none \
  --dns 127.0.0.1 \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 512 \
  --memory 2g \
  --cpus 2 \
  --tmpfs /tmp:rw,nosuid,nodev,exec,size=768m,mode=1777 \
  --mount "type=bind,src=$output,dst=/workspace/artifacts" \
  "$image"
