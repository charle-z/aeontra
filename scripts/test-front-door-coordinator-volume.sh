#!/bin/sh
set -eu

volume="mcp-front-door-coordinator-ci-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
container="mcp-front-door-coordinator-ci-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
secret="volume-test-secret-value"
cleanup() {
    docker rm --force "$container" >/dev/null 2>&1 || true
    docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker volume create "$volume" >/dev/null
docker run --detach --name "$container" \
    --volume "$volume:/coordinator-state" \
    --env COOLIFY_URL=https://control.example \
    --env COOLIFY_API_TOKEN="$secret" \
    --env MCP_FRONT_DOOR_COORDINATOR_APP_UUID=coord1 \
    --env MCP_FRONT_DOOR_APP_UUID=front1 \
    --env MCP_FRONT_DOOR_BACKEND_APP_UUID=backend1 \
    --env MCP_FRONT_DOOR_EXPECTED_COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
    --env MCP_FRONT_DOOR_EXPECTED_BACKEND_COMMIT=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb \
    --env MCP_FRONT_DOOR_EXPECTED_PROTOCOL=2024-11-05 \
    --env MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc \
    --env MCP_FRONT_DOOR_COORDINATOR_TARGET=idle \
    mcp-front-door-coordinator:ci >/dev/null

attempt=0
while [ "$attempt" -lt 30 ]; do
    if [ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]; then
        docker logs "$container"
        exit 1
    fi
    if docker exec "$container" wget -qO- http://127.0.0.1:8766/healthz | grep -qx 'ok mcp-front-door-coordinator'; then
        break
    fi
    attempt=$((attempt + 1))
    sleep 1
done

test "$attempt" -lt 30
test "$(docker exec "$container" awk '/^Uid:/ {print $2; exit}' /proc/1/status)" = 10003
docker exec "$container" su-exec 10003:10003 sh -c 'test -w /coordinator-state && : > /coordinator-state/.write-probe && rm /coordinator-state/.write-probe'

ready_attempt=0
while [ "$ready_attempt" -lt 45 ]; do
    ready_headers="$(docker exec "$container" sh -c 'wget -S -O /tmp/ready-body http://127.0.0.1:8766/readyz 2>&1 || true')"
    if printf '%s\n' "$ready_headers" | grep -q '503 Service Unavailable' \
        && docker exec "$container" grep -q '"code":"topology_validation_failed"' /tmp/ready-body; then
        break
    fi
    ready_attempt=$((ready_attempt + 1))
    sleep 1
done
test "$ready_attempt" -lt 45
if docker exec "$container" grep -Fq "$secret" /tmp/ready-body; then
    echo "readiness exposed a secret" >&2
    exit 1
fi
logs="$(docker logs "$container" 2>&1)"
printf '%s\n' "$logs" | grep -q 'code=topology_validation_failed'
if printf '%s\n' "$logs" | grep -Fq "$secret"; then
    echo "logs exposed a secret" >&2
    exit 1
fi

test "$(docker inspect --format '{{.State.Health.Status}}' "$container")" != healthy
docker stop --time 15 "$container" >/dev/null
test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0
docker run --rm --entrypoint /bin/sh \
    --volume "$volume:/coordinator-state:ro" \
    mcp-front-door-coordinator:ci -c \
    'test -f /coordinator-state/front-door-coordinator.json && grep -Eq '"'"'"revision":[[:space:]]*0'"'"' /coordinator-state/front-door-coordinator.json && grep -Eq '"'"'"state":[[:space:]]*"idle"'"'"' /coordinator-state/front-door-coordinator.json'
