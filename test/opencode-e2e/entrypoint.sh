#!/bin/sh
set -eu

/usr/local/bin/mcp-devbox-bubblewrap-e2e -test.run=TestBubblewrapRealIsolationSmoke -test.count=1 -test.v

exec /usr/local/bin/mcp-devbox-opencode-e2e "$@"
