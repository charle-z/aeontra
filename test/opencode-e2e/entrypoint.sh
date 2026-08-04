#!/bin/sh
set -eu

combined='-test.run=TestOpenCodeExternalModelVerticalSlice|TestRemoteOpenCodeDistributedRelay'
if [ "${1:-}" = "$combined" ]; then
  shift
  /usr/local/bin/mcp-devbox-opencode-e2e \
    -test.run='^TestOpenCodeExternalModelVerticalSlice$' "$@"
  exec /usr/local/bin/mcp-devbox-opencode-e2e \
    -test.run='^TestRemoteOpenCodeDistributedRelay$' "$@"
fi

exec /usr/local/bin/mcp-devbox-opencode-e2e "$@"
