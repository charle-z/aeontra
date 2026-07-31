#!/bin/sh
set -eu

volume="mcp-front-door-coordinator-ci-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
container="mcp-front-door-coordinator-ci-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
cleanup() {
    docker rm --force "$container" >/dev/null 2>&1 || true
    docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker volume create "$volume" >/dev/null
docker run --detach --name "$container" \
    --volume "$volume:/coordinator-state" \
    --env COOLIFY_URL=https://control.example \
    --env COOLIFY_API_TOKEN=ci-token \
    --env MCP_FRONT_DOOR_COORDINATOR_APP_UUID=coord1 \
    --env MCP_FRONT_DOOR_APP_UUID=front1 \
    --env MCP_FRONT_DOOR_BACKEND_APP_UUID=backend1 \
    --env MCP_FRONT_DOOR_EXPECTED_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    --env MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
    --env MCP_FRONT_DOOR_EXPECTED_PROTOCOL=2024-11-05 \
    --env MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
    --env MCP_FRONT_DOOR_COORDINATOR_TARGET=idle \
    mcp-front-door-coordinator:ci >/dev/null

health=starting
attempt=0
while [ "$attempt" -lt 30 ]; do
    health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$container")"
    [ "$health" = healthy ] && break
    if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then
        docker logs "$container"
        exit 1
    fi
    attempt=$((attempt + 1))
    sleep 1
done

test "$health" = healthy
test "$(docker exec "$container" awk '/^Uid:/ {print $2; exit}' /proc/1/status)" = 10003
docker exec "$container" su-exec 10003:10003 sh -c 'test -w /coordinator-state && : > /coordinator-state/.write-probe && rm /coordinator-state/.write-probe'
