#!/bin/sh
set -eu
umask 077

fail() {
  printf 'clean-install: %s\n' "$1" >&2
  exit 1
}

for tool in go git grep mktemp rm; do
  command -v "$tool" >/dev/null 2>&1 || fail "required tool is missing: $tool"
done

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
WORK=$(mktemp -d "${TMPDIR:-/tmp}/aeontra-clean-install.XXXXXX")

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  case "$WORK" in
    "${TMPDIR:-/tmp}"/aeontra-clean-install.*) rm -rf -- "$WORK" ;;
    *) printf 'clean-install: refused unsafe cleanup path\n' >&2 ;;
  esac
  exit "$status"
}

trap cleanup EXIT HUP INT TERM

REPOSITORY=$WORK/repository
STATE=$WORK/state
BINARY=$WORK/mcp-devbox
mkdir -m 0700 "$REPOSITORY" "$STATE"
git -C "$REPOSITORY" init --quiet
printf 'clean install fixture\n' > "$REPOSITORY/README.md"

(
  cd "$ROOT"
  CGO_ENABLED=0 go build -trimpath -o "$BINARY" ./cmd/mcp-devbox
)

"$BINARY" version | grep -Eq '^mcp-devbox [^ ]+ \(commit [^)]+\)$' ||
  fail 'version output is invalid'

INITIALIZE='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"clean-install","version":"1"}}}'
INITIALIZED='{"jsonrpc":"2.0","method":"notifications/initialized"}'
READ_FIXTURE='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"README.md"}}}'
RESPONSE=$(
  printf '%s\n%s\n%s\n' "$INITIALIZE" "$INITIALIZED" "$READ_FIXTURE" |
    MCP_DEVBOX_STATE_ROOT="$STATE" "$BINARY" serve \
      --root "$REPOSITORY" \
      --mode read-only \
      --audit "$STATE/audit.jsonl" \
      --observability off
)

printf '%s\n' "$RESPONSE" | grep -F '"protocolVersion":"2024-11-05"' >/dev/null ||
  fail 'stdio initialize did not return the expected protocol'
printf '%s\n' "$RESPONSE" | grep -F '"serverInfo"' >/dev/null ||
  fail 'stdio initialize did not return server identity'
printf '%s\n' "$RESPONSE" | grep -F 'clean install fixture' >/dev/null ||
  fail 'stdio read-only tool call did not complete'

test -s "$STATE/audit.jsonl" || fail 'audit log was not created'
printf 'clean-install: PASS\n'
