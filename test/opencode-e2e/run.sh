#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
image=${OPENCODE_E2E_IMAGE:-mcp-devbox-opencode-e2e:local}
output=${OPENCODE_E2E_OUTPUT:-"$repo_root/artifacts"}
container="mcp-devbox-opencode-e2e-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-0}"
staging=$(mktemp -d "${TMPDIR:-/tmp}/mcp-devbox-opencode-e2e.XXXXXX")
git_tree=$(git -C "$repo_root" write-tree)
git_commit=$(git -C "$repo_root" rev-parse HEAD)

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$staging"
}

trap cleanup EXIT HUP INT TERM

mkdir -p "$output"
chmod 0777 "$staging"

timeout --signal=TERM --kill-after=10s 12m \
  docker build --no-cache --pull \
    --file "$repo_root/test/opencode-e2e/Dockerfile" \
    --build-arg "P11_2_GIT_TREE=$git_tree" \
    --build-arg "P11_2_GIT_COMMIT=$git_commit" \
    --tag "$image" \
    "$repo_root"

set +e
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
  --mount "type=bind,src=$staging,dst=/workspace/artifacts" \
  "$image"
status=$?
set -e

for report in \
  opencode-e2e-report.json \
  opencode-relay-container-report.json
do
  if [ -s "$staging/$report" ]; then
    install -m 0644 "$staging/$report" "$output/$report"
  fi
done

exit "$status"
