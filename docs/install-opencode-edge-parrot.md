# Install and verify the Trusted Linux Workcell on Parrot WSL2

Status: verified human-operated onboarding for P12. The first real remote smoke
completed on 2026-07-18 from Parrot WSL2 through the CubePath-hosted control plane.
The validated P12 merge is `3946fd7033f28906deb932298387034e2fa27fe8`. Use a newer
commit only after its exact checks are green and it contains the onboarding
hardening described here.

This guide never creates a pairing code automatically, never stores a pairing code,
and never grants the model sudo. The operator performs installation and pairing.

## Proven result

The validated process boundary is:

```text
CubePath-hosted MCP Devbox control plane
└── outbound signed HTTPS
    └── mcp-edge on Parrot as a non-root user
        ├── model-turn-driver over a private Unix socket
        └── Bubblewrap
            └── pinned OpenCode 1.18.1
```

The successful smoke used workspace `ws_7c4686f5d9244bbad30ae705d4b660c5`,
completed six model-turn sequences, created the exact requested file, and passed
`git diff --check`. The workcell-owned `.mcp-devbox/` directory is expected. The
service maintains bounded five-second heartbeat updates while a runtime is active.

## 1. Prerequisites

Run inside a dedicated Parrot WSL2 distribution with systemd:

```bash
ps -p 1 -o comm=
uname -a
```

PID 1 must be `systemd`. Keep workspaces in the Linux filesystem, never under
`/mnt/c` or `/mnt/d`.

Install reviewed host dependencies interactively:

```bash
sudo apt-get update
sudo apt-get install --yes --no-install-recommends \
  bubblewrap ca-certificates git python3 python3-venv pipx \
  build-essential pkg-config iproute2 curl wget openssl ripgrep podman
```

The validated toolchain is Go 1.26.6, Node 24.18.0, npm 11.16.0, Bubblewrap 0.11.0,
and Podman 5.4.2. Install Go and Node from reviewed archives or a reviewed package
source. Root-owned wrapper files in `/usr/local/bin` are preferable to symlinks
when the local inventory must resolve the executable itself inside an allowlisted
path.

Create private roots as the Edge user:

```bash
install -d -m 0700 \
  "$HOME/.local/state/mcp-edge" \
  "$HOME/workspaces" \
  "$HOME/htb-machines"
```

## 2. Build one reviewed source commit

```bash
rm -rf /tmp/mcp-devbox-reviewed
git clone https://github.com/charle-z/mcp-devbox.git /tmp/mcp-devbox-reviewed
cd /tmp/mcp-devbox-reviewed
git checkout <REVIEWED_COMMIT>
git status --short
go test -p 1 ./... -count=1
go build -trimpath -o /tmp/mcp-edge ./cmd/mcp-edge
go build -trimpath -o /tmp/model-turn-driver ./cmd/model-turn-driver
sudo install -o root -g root -m 0755 /tmp/mcp-edge /usr/local/bin/mcp-edge
sudo install -d -o root -g root -m 0755 /usr/local/libexec/mcp-devbox
sudo install -o root -g root -m 0755 /tmp/model-turn-driver \
  /usr/local/libexec/mcp-devbox/model-turn-driver
```

Record hashes locally:

```bash
sha256sum /usr/local/bin/mcp-edge \
  /usr/local/libexec/mcp-devbox/model-turn-driver
```

On WSL, `/etc/resolv.conf` may resolve to `/mnt/wsl/resolv.conf`. That is a WSL
system file, not a Windows workspace. Tests and validators must reject only
`/mnt/c`, `/mnt/d`, and their descendants.

## 3. Install pinned OpenCode and the provider

Install OpenCode from the reviewed lockfile:

```bash
cd /tmp/mcp-devbox-reviewed/test/opencode-e2e
npm ci --no-audit --no-fund
./node_modules/.bin/opencode --version
sudo install -d -o root -g root -m 0755 /opt/mcp-devbox/opencode-1.18.1
sudo install -o root -g root -m 0755 \
  node_modules/opencode-linux-x64/bin/opencode \
  /opt/mcp-devbox/opencode-1.18.1/opencode
sudo install -o root -g root -m 0644 package-lock.json \
  /opt/mcp-devbox/opencode-1.18.1/package-lock.json
```

The provider has no dependency lockfile and no npm test script. Its correct test is:

```bash
cd /tmp/mcp-devbox-reviewed/integrations/opencode/provider
node --test provider.test.mjs
sudo install -d -o root -g root -m 0755 /opt/mcp-devbox/opencode-provider
sudo install -o root -g root -m 0644 package.json index.js htb-actions.js dev-actions.js \
  /opt/mcp-devbox/opencode-provider/
```

Confirm the provider export and identity:

```bash
node --input-type=module -e '
  const provider = await import("file:///opt/mcp-devbox/opencode-provider/index.js");
  if (typeof provider.createMCPDevboxModelBridge !== "function") throw new Error("provider export missing");
'
node -e 'const p=require("/opt/mcp-devbox/opencode-provider/package.json"); console.log(p.name,p.version)'
```

## 4. Enable optional rootless Podman

Never add the Edge user to a rootful Docker group and never expose
`/var/run/docker.sock` or `/run/docker.sock`.

```bash
systemctl --user enable --now podman.socket
stat -c '%a %U:%G %F %n' "$XDG_RUNTIME_DIR/podman/podman.sock"
podman --url "unix://$XDG_RUNTIME_DIR/podman/podman.sock" info
```

The socket must be below `/run/user/<uid>`, owned by the Edge user, not symlinked,
and inaccessible to other users.

## 5. Run the packaged onboarding preflight

```bash
cd /tmp/mcp-devbox-reviewed
MCP_DEVBOX_REQUIRE_ROOTLESS=1 \
  bash packaging/parrot/onboarding-preflight.sh
```

Expected final line:

```text
parrot-onboarding-preflight-ok rootless=yes engine=podman
```

This proves the real mounts, host-shared network declaration, WSL resolver,
workspace/runtime writes, hidden home and Windows mounts, pinned OpenCode, provider,
Node/Go, and the rootless API socket. Stop before pairing if it fails.

## 6. Pair through stdin

Create a one-use code only in the private production terminal:

```bash
mcp-devbox edge pairing-create --state-root /state --ttl 10m
```

Consume it on Parrot without putting it in argv or history:

```bash
read -rsp 'One-time pairing code: ' EDGE_PAIR_CODE; printf '\n'
printf '%s\n' "$EDGE_PAIR_CODE" | mcp-edge pair \
  --server https://mcp-devbox-charlez.duckdns.org \
  --state "$HOME/.local/state/mcp-edge" \
  --name parrot-trusted-linux
unset EDGE_PAIR_CODE
```

The state root must remain `0700`; identity and key files must remain `0600`.

## 7. Register a disposable first workspace

```bash
WORKSPACE="$HOME/workspaces/p12-smoke"
rm -rf "$WORKSPACE"
install -d -m 0700 "$WORKSPACE"
git -C "$WORKSPACE" init -b main
printf '# P12 smoke\n' >"$WORKSPACE/README.md"
git -C "$WORKSPACE" add README.md
git -C "$WORKSPACE" -c user.name='P12 Smoke' \
  -c user.email='p12-smoke@localhost' commit -m 'Initial smoke fixture'

ADD_OUTPUT="$(mcp-edge workspace add \
  --state "$HOME/.local/state/mcp-edge" \
  --profile linux-workcell "$WORKSPACE")"
printf '%s\n' "$ADD_OUTPUT"
WORKSPACE_ID="$(printf '%s\n' "$ADD_OUTPUT" | cut -d' ' -f2)"
mcp-edge workspace configure --state "$HOME/.local/state/mcp-edge" \
  --mode dev "$WORKSPACE_ID"
mcp-edge workspace inventory --state "$HOME/.local/state/mcp-edge" \
  "$WORKSPACE_ID"
```

## 8. Install the packaged systemd unit

```bash
sudo install -o root -g root -m 0644 \
  packaging/systemd/mcp-devbox-opencode-edge@.service \
  /etc/systemd/system/mcp-devbox-opencode-edge@.service
sudo systemd-analyze verify /etc/systemd/system/mcp-devbox-opencode-edge@.service
sudo systemctl daemon-reload
sudo systemctl enable --now "mcp-devbox-opencode-edge@$(id -un).service"
```

Bubblewrap needs `AF_NETLINK` to create its loopback interface through
`NETLINK_ROUTE`. The packaged unit allows only `AF_UNIX`, `AF_INET`,
`AF_INET6`, and `AF_NETLINK`:

```bash
systemctl show "mcp-devbox-opencode-edge@$(id -un).service" \
  -p RestrictAddressFamilies
systemctl status "mcp-devbox-opencode-edge@$(id -un).service" --no-pager -l
```

Without `AF_NETLINK`, Bubblewrap fails with the stable safe code
`bubblewrap_netlink_route_denied`.

## 9. First remote smoke

Use one unique objective and exactly one runtime. Ask the remote agent to confirm
Git and `README.md`, create `edge-smoke.txt`, verify exact contents,
`git status --short`, and `git diff --check`, without commit, push,
dependencies, containers, or services.

Expected local result:

```text
?? .mcp-devbox/
?? edge-smoke.txt
```

A later runtime may intentionally repeat a completed objective. The local journal
still prevents two active runtimes in one workspace, but no longer treats a
terminal goal digest as a permanent ban.

## 10. Continue a registered workspace without a prompt

After `lab init` has prepared a machine once, later chats should not reconstruct or
transport its operational objective. Use `workspace_runtime_continue` with only the
opaque workspace ID and a bounded timeout:

```text
workspace_runtime_continue(
  workspace_id="ws_...",
  timeout_seconds=3600,
  idempotency_key="new-random-key-for-this-request"
)
```

The server resolves the paired Edge from its signed opaque workspace registration,
uses the fixed `resume-local-contract-v1` objective, and creates at most one active
runtime for that workspace. The Edge reads `.mcp-devbox/instructions.md` and
`.mcp-devbox/current-state.md` locally. The call carries no target, IP, machine,
credential, flag, command, checkpoint, path, or free-form instruction and is never
retried automatically. See `docs/workspace-runtime-continuation.md`.

For private development repositories, configure the Edge GitHub authority from stdin
after installation and keep the public GitHub API copy in Coolify. The exact commands,
permissions, broker boundary, and ready-to-use ChatGPT prompt are in
`docs/development-edge-git.md`.

## 11. Diagnostics and recovery

Inspect bounded safe codes only:

```bash
sudo journalctl -u "mcp-devbox-opencode-edge@$(id -un).service" \
  --since -10m --no-pager -o cat
```

Important codes include `bubblewrap_netlink_route_denied`, namespace/map/mount
failures, installation integrity/version/provider/driver failures, socket failures,
and OpenCode provider/driver failures. Raw prompts, credentials, signatures, and
host paths must not be logged.

## 12. Kill switch, Revocation, Rollback, and Uninstall

Create `$HOME/.local/state/mcp-edge/STOP` and stop the service for a local Kill switch.
Revoke the device independently from production before deleting identity. Update
Edge, driver, OpenCode/lockfile, and provider as one reviewed set; preserve old
hashes for Rollback.

For Uninstall, disable and remove the template instance and unit, then remove the
root-owned Edge/driver/OpenCode/provider files. Delete the private state only after
Revocation and any required audit retention. Workspaces are a separate human
decision and must not be silently deleted.
