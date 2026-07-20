#!/usr/bin/env bash
set -euo pipefail

STATE_ROOT="${MCP_DEVBOX_STATE_ROOT:-${HOME}/.local/state/mcp-edge}"
DEV_ROOT="${MCP_DEVBOX_DEV_ROOT:-${HOME}/workspaces}"
HTB_ROOT="${MCP_DEVBOX_HTB_ROOT:-${HOME}/htb-machines}"
OPENCODE="${MCP_DEVBOX_OPENCODE:-/opt/mcp-devbox/opencode-1.18.1/opencode}"
INTEGRITY="${MCP_DEVBOX_INTEGRITY:-/opt/mcp-devbox/opencode-1.18.1/package-lock.json}"
PROVIDER="${MCP_DEVBOX_PROVIDER:-/opt/mcp-devbox/opencode-provider}"
DRIVER="${MCP_DEVBOX_DRIVER:-/usr/local/libexec/mcp-devbox/model-turn-driver}"
BWRAP="${MCP_DEVBOX_BWRAP:-/usr/bin/bwrap}"
BUNDLE_ROOT="${MCP_DEVBOX_BUNDLE_ROOT:-/opt/mcp-devbox/current}"
REQUIRE_ROOTLESS="${MCP_DEVBOX_REQUIRE_ROOTLESS:-0}"

fail() {
  printf 'parrot-onboarding-preflight: %s\n' "$*" >&2
  exit 1
}

[ "$(id -u)" -ne 0 ] || fail "run as the future Edge user, not root"
[ "$(ps -p 1 -o comm= | tr -d '[:space:]')" = "systemd" ] || fail "PID 1 is not systemd"

for command in bwrap curl git go node npm python3; do
  command -v "$command" >/dev/null 2>&1 || fail "missing command: $command"
done

for path in /usr/local/bin/mcp-edge "$DRIVER" "$OPENCODE" "$INTEGRITY" "$PROVIDER/index.js" "$PROVIDER/package.json" "$BUNDLE_ROOT/manifest.json" "$BUNDLE_ROOT/manifest.sig" "$BUNDLE_ROOT/libexec/mcp-autopilot-worker" "$BUNDLE_ROOT/libexec/mcp-bundle-updater"; do
  [ -e "$path" ] || fail "missing reviewed installation path: $path"
done
[ -x /usr/local/bin/mcp-edge ] || fail "mcp-edge is not executable"
[ -x "$DRIVER" ] || fail "model-turn-driver is not executable"
[ -x "$OPENCODE" ] || fail "OpenCode is not executable"
[ -x "$BWRAP" ] || fail "Bubblewrap is not executable"

[ "$(node --version)" = "v24.18.0" ] || fail "Node must be v24.18.0"
[ "$("$OPENCODE" --version)" = "1.18.1" ] || fail "OpenCode must be 1.18.1"

node --input-type=module -e '
  const provider = await import("file:///opt/mcp-devbox/opencode-provider/index.js");
  if (typeof provider.createMCPDevboxModelBridge !== "function") {
    throw new Error("provider export missing");
  }
' >/dev/null

install -d -m 0700 "$STATE_ROOT" "$DEV_ROOT" "$HTB_ROOT"
for root in "$STATE_ROOT" "$DEV_ROOT" "$HTB_ROOT"; do
  [ ! -L "$root" ] || fail "symlinked root rejected: $root"
  resolved="$(readlink -f "$root")"
  case "$resolved" in
    /mnt/c|/mnt/c/*|/mnt/d|/mnt/d/*) fail "Windows-mounted root rejected: $resolved" ;;
  esac
done

WORKSPACE="${DEV_ROOT}/.mcp-devbox-onboarding-preflight"
RUNTIME="${STATE_ROOT}/.onboarding-preflight-runtime"
rm -rf "$WORKSPACE" "$RUNTIME"
trap 'rm -rf "$WORKSPACE" "$RUNTIME"' EXIT
install -d -m 0700 "$WORKSPACE" "$RUNTIME"
printf 'host-ok\n' >"$WORKSPACE/host.txt"

SOCKET=""
ENGINE=""
uid="$(id -u)"
for candidate in "/run/user/${uid}/docker.sock" "/run/user/${uid}/podman/podman.sock"; do
  [ -S "$candidate" ] || continue
  [ "$(stat -c '%u' "$candidate")" = "$uid" ] || fail "rootless socket is not owned by the Edge user"
  socket_mode="$(stat -c '%a' "$candidate")"
  (( (8#$socket_mode & 7) == 0 )) || fail "rootless socket is accessible to other users"
  SOCKET="$candidate"
  case "$candidate" in
    */podman/*) ENGINE="podman" ;;
    *) ENGINE="docker" ;;
  esac
  break
done
if [ "$REQUIRE_ROOTLESS" = "1" ] && [ -z "$SOCKET" ]; then
  fail "no user-owned rootless Docker or Podman socket found"
fi

host_netns="$(readlink /proc/self/ns/net)"
common=(
  --die-with-parent
  --new-session
  --unshare-all
  --share-net
  --clearenv
)
for path in /usr /bin /sbin /lib /lib64 /etc/ssl/certs /etc/ca-certificates; do
  [ ! -e "$path" ] || common+=(--ro-bind "$path" "$path")
done
for target in /etc/resolv.conf /etc/hosts /etc/nsswitch.conf /etc/passwd /etc/group /etc/services /etc/protocols; do
  [ -e "$target" ] || continue
  source="$(readlink -f "$target")"
  [ -f "$source" ] || continue
  common+=(--ro-bind "$source" "$target")
done

args=(
  "${common[@]}"
  --proc /proc
  --dev /dev
  --tmpfs /tmp
  --bind "$WORKSPACE" /workspace
  --bind "$RUNTIME" /runtime
  --ro-bind "$PROVIDER" /mcp-provider
  --ro-bind "$OPENCODE" /mcp-opencode
)
if [ -n "$SOCKET" ]; then
  args+=(--bind "$SOCKET" /runtime/rootless-container.sock)
fi
args+=(
  --setenv PATH /usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
  --setenv HOME /runtime/home
  --setenv HOST_NETNS "$host_netns"
  --setenv EDGE_HOME "$HOME"
  --setenv ROOTLESS_ENABLED "$([ -n "$SOCKET" ] && printf 1 || printf 0)"
  --
  /bin/bash -euo pipefail -c '
    install -d -m 0700 /runtime/home
    test "$(readlink /proc/self/ns/net)" = "$HOST_NETNS"
    test -s /etc/resolv.conf
    grep -qx host-ok /workspace/host.txt
    printf "container-ok\n" >/workspace/container.txt
    test ! -e /root
    test ! -e "$EDGE_HOME"
    test ! -e /mnt/c
    test ! -e /mnt/d
    test ! -S /run/docker.sock
    test ! -S /var/run/docker.sock
    test "$(node --version)" = v24.18.0
    test "$(/mcp-opencode --version)" = 1.18.1
    go version >/dev/null
    node --input-type=module -e "
      const provider = await import(\"file:///mcp-provider/index.js\");
      if (typeof provider.createMCPDevboxModelBridge !== \"function\") {
        throw new Error(\"provider export missing\");
      }
    "
    if [ "$ROOTLESS_ENABLED" = 1 ]; then
      test -S /runtime/rootless-container.sock
      response="$(curl --fail --silent --show-error --max-time 5 --unix-socket /runtime/rootless-container.sock http://localhost/_ping)"
      test "$response" = OK
    fi
  '
)

"$BWRAP" "${args[@]}"
[ "$(cat "$WORKSPACE/container.txt")" = "container-ok" ] || fail "workspace write did not survive Bubblewrap"
printf 'parrot-onboarding-preflight-ok rootless=%s engine=%s\n' "$([ -n "$SOCKET" ] && printf yes || printf no)" "${ENGINE:-none}"
