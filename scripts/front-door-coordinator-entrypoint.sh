#!/bin/sh
set -eu

state_root="${MCP_FRONT_DOOR_COORDINATOR_STATE_ROOT:-/coordinator-state}"
if [ "$state_root" != "/coordinator-state" ]; then
    echo "front-door coordinator state root must remain /coordinator-state" >&2
    exit 1
fi
if [ -L "$state_root" ]; then
    echo "front-door coordinator state root must not be a symlink" >&2
    exit 1
fi

install -d -m 0700 -o 10003 -g 10003 "$state_root"
chown 10003:10003 "$state_root"
chmod 0700 "$state_root"

journal="$state_root/front-door-transition.json"
if [ -e "$journal" ]; then
    if [ ! -f "$journal" ] || [ -L "$journal" ]; then
        echo "front-door coordinator journal must be a regular file" >&2
        exit 1
    fi
    chown 10003:10003 "$journal"
    chmod 0600 "$journal"
fi

exec su-exec 10003:10003 /usr/local/bin/mcp-front-door-coordinator
