# Install the OpenCode Edge on Parrot WSL2

Status: human-operated installation guide for P11.2. This procedure does not
perform pairing, modify Coolify, deploy the VPS, install a VPN, or grant access to
Docker. Stop unless the reviewed P11.2 commit and its required GitHub Actions
checks are green.

## Resulting boundary

The intended process tree is:

```text
mcp-edge (dedicated Parrot host process)
└── model-turn-driver (dedicated local process)
    └── Bubblewrap (host executable)
        └── OpenCode 1.18.1 (the only process inside the sandbox)
```

The server, Edge, driver, and OpenCode remain separate processes. Edge makes only
outbound signed HTTPS requests. The driver exposes one local Unix socket. OpenCode
has no TCP listener and enters Bubblewrap with `--die-with-parent`,
`--new-session`, `--unshare-all`, and `--clearenv`.

The sandbox exposes only:

- `/workspace`, read/write;
- `/runtime`, read/write and private;
- the external-driver provider, read-only;
- the pinned OpenCode executable, read-only;
- the minimum read-only system runtime paths needed by the executable;
- `/proc`, `/dev`, and an ephemeral `/tmp`.

It does not expose the Edge identity/state, the host home, `/root`, `/mnt/c`,
`/mnt/d`, SSH keys, browser profiles, VPN files, or a Docker socket. There is no
fallback that starts OpenCode without Bubblewrap.

## 1. Prepare a dedicated Parrot WSL2 distribution

Use a separate Parrot WSL2 distribution. Do not reuse a personal Linux distro or
a workspace mounted from Windows. Confirm WSL2 and systemd inside Parrot:

```bash
uname -a
systemctl is-system-running || true
ps -p 1 -o comm=
```

PID 1 must be `systemd`. If it is not, enable systemd in `/etc/wsl.conf`, stop the
distribution from PowerShell with `wsl --shutdown`, then start it again:

```ini
[boot]
systemd=true
```

Do not continue with WSL1.

## 2. Install reviewed host dependencies

Use only Parrot/Debian package repositories already approved for the distro. Do
not pipe a remote script into a shell and do not let the running service invoke
`apt` or `sudo`.

```bash
sudo apt-get update
sudo apt-get install --yes --no-install-recommends \
  bubblewrap ca-certificates git golang-go nodejs npm ripgrep
```

Verify the tools explicitly:

```bash
bwrap --version
node --version
npm --version
go version
git --version
rg --version
```

The reviewed P11.2 build uses Node 24 for installation/tests. If the installed
Node major is not 24, stop and provision Node 24 through a separately reviewed
package source. Do not silently change the project or OpenCode version.

## 3. Create the dedicated account and directories

Create a dedicated account with no sudo, Docker, `adm`, `dialout`, or other
supplementary groups:

```bash
sudo useradd --system --create-home \
  --home-dir /var/lib/mcp-devbox-edge \
  --shell /usr/sbin/nologin mcpedge
id -nG mcpedge
test "$(id -nG mcpedge)" = "mcpedge"
```

Create the private state, socket, installation, and workspace roots:

```bash
sudo install -d -o mcpedge -g mcpedge -m 0700 /var/lib/mcp-devbox-edge
sudo install -d -o mcpedge -g mcpedge -m 0700 /var/lib/mcp-devbox-edge/opencode
sudo install -d -o mcpedge -g mcpedge -m 0700 /srv/mcp-devbox-workspaces
sudo install -d -o root -g root -m 0755 /opt/mcp-devbox
sudo install -d -o root -g root -m 0755 /opt/mcp-devbox/opencode-1.18.1
sudo install -d -o root -g root -m 0755 /opt/mcp-devbox/opencode-provider
sudo install -d -o root -g root -m 0755 /usr/local/libexec/mcp-devbox
```

The service must not join the `docker` group. Do not mount `/var/run/docker.sock`
or `/run/docker.sock` into any namespace.

## 4. Build reviewed Edge and driver binaries

Clone the source into a temporary administrator-owned directory, check out the
reviewed commit, verify it, build without embedding local paths, and install the
binaries root-owned:

```bash
git clone https://github.com/charle-z/mcp-devbox.git /tmp/mcp-devbox-p11.2
cd /tmp/mcp-devbox-p11.2
git checkout <REVIEWED_P11_2_COMMIT>
git status --short
go test -p 1 ./... -count=1
go build -trimpath -o /tmp/mcp-edge ./cmd/mcp-edge
go build -trimpath -o /tmp/model-turn-driver ./cmd/model-turn-driver
sudo install -o root -g root -m 0755 /tmp/mcp-edge /usr/local/bin/mcp-edge
sudo install -o root -g root -m 0755 /tmp/model-turn-driver \
  /usr/local/libexec/mcp-devbox/model-turn-driver
```

Record the installed hashes in a local administrator log:

```bash
sha256sum /usr/local/bin/mcp-edge \
  /usr/local/libexec/mcp-devbox/model-turn-driver
```

## 5. Install pinned OpenCode 1.18.1 and verify integrity

Use the reviewed lockfile in `test/opencode-e2e`. The launcher checks all three
of these values before accepting work:

- package: `opencode-ai`;
- version: `1.18.1`;
- npm integrity:
  `sha512-Rtp0fCJyu3Iz0MXfwQeAYdYjIsSPPUWYyJO0mf0Q9v5zTNYxlakzXUh+Van50XAmEDAhCaJvCcOJzweq2k3HMQ==`.

Install from the lockfile without changing it:

```bash
cd /tmp/mcp-devbox-p11.2/test/opencode-e2e
npm ci --no-audit --no-fund
./node_modules/.bin/opencode --version
```

The output must be exactly `1.18.1`. Install the native executable and the exact
lockfile read-only:

```bash
sudo install -o root -g root -m 0755 \
  node_modules/opencode-linux-x64/bin/opencode \
  /opt/mcp-devbox/opencode-1.18.1/opencode
sudo install -o root -g root -m 0644 package-lock.json \
  /opt/mcp-devbox/opencode-1.18.1/package-lock.json
/opt/mcp-devbox/opencode-1.18.1/opencode --version
```

Do not replace the binary or lockfile independently. An update is a reviewed pair.

## 6. Install the local provider read-only

Install the provider package from the same reviewed tree. Preserve its package
metadata and compiled JavaScript exactly as tested:

```bash
cd /tmp/mcp-devbox-p11.2/integrations/opencode/provider
npm ci --no-audit --no-fund
npm test
sudo cp -a . /opt/mcp-devbox/opencode-provider.new
sudo chown -R root:root /opt/mcp-devbox/opencode-provider.new
sudo find /opt/mcp-devbox/opencode-provider.new -type d -exec chmod 0755 {} +
sudo find /opt/mcp-devbox/opencode-provider.new -type f -exec chmod 0644 {} +
sudo rm -rf /opt/mcp-devbox/opencode-provider
sudo mv /opt/mcp-devbox/opencode-provider.new /opt/mcp-devbox/opencode-provider
```

Confirm the package identity:

```bash
node -e 'const p=require("/opt/mcp-devbox/opencode-provider/package.json"); console.log(p.name,p.version)'
```

Expected: `@mcp-devbox/opencode-external-driver 0.1.0`.

## 7. Mandatory Bubblewrap preflight before pairing

Run this as `mcpedge`, directly on the Parrot host. It incrementally proves the
same primitives required by the production launcher. It must finish with
`bubblewrap-preflight-ok`:

```bash
sudo -u mcpedge bash --noprofile --norc <<'BWRAP_PREFLIGHT'
set -euo pipefail
root=$(mktemp -d /var/lib/mcp-devbox-edge/bwrap-preflight.XXXXXX)
trap 'rm -rf "$root"' EXIT
install -d -m 0700 "$root/workspace" "$root/runtime"
printf '{}\n' >"$root/provider.json"
chmod 0600 "$root/provider.json"

bwrap --version

common=(--die-with-parent --new-session --unshare-all --clearenv)
for path in /usr /bin /sbin /lib /lib64 /etc/ssl/certs /etc/ca-certificates; do
  [ ! -e "$path" ] || common+=(--ro-bind "$path" "$path")
done

bwrap "${common[@]}" \
  --proc /proc --dev /dev --tmpfs /tmp \
  --bind "$root/workspace" /workspace \
  --bind "$root/runtime" /runtime \
  --ro-bind "$root/provider.json" /provider.json \
  --setenv PATH /usr/bin:/bin \
  --setenv HOME /runtime/home \
  -- \
  /bin/sh -eu -c '
    mkdir -m 700 /runtime/home
    printf ok >/workspace/write-test
    printf ok >/runtime/write-test
    test -s /workspace/write-test
    test -s /runtime/write-test
    ! chmod u+w /provider.json
    test ! -e /root
    test ! -e /mnt/c
    test ! -e /mnt/d
    test ! -S /run/docker.sock
    test ! -S /var/run/docker.sock
    ! /bin/bash -c "exec 3<>/dev/tcp/1.1.1.1/443" 2>/dev/null
  '

printf 'bubblewrap-preflight-ok\n'
BWRAP_PREFLIGHT
```

If any stage fails, stop before pairing. Do not use `--privileged`, disable
AppArmor globally, change a global sysctl, expose a Docker socket, add host
network/PID/IPC, or configure a no-Bubblewrap fallback.

## 8. Register Linux-native workspaces

Clone repositories inside the Parrot Linux filesystem. Never register `/`,
`/mnt`, `/mnt/c`, `/mnt/d`, a symlink to those paths, the private state root, or a
parent/child overlap with the state root.

```bash
sudo -u mcpedge git clone <REPOSITORY_URL> \
  /srv/mcp-devbox-workspaces/<WORKSPACE_NAME>
sudo -u mcpedge /usr/local/bin/mcp-edge workspace add \
  --state /var/lib/mcp-devbox-edge \
  --path /srv/mcp-devbox-workspaces/<WORKSPACE_NAME>
sudo -u mcpedge /usr/local/bin/mcp-edge workspace list \
  --state /var/lib/mcp-devbox-edge
```

The command returns an opaque workspace id. The VPS receives that id, not a host
path.

## 9. Pair only after preflight and server approval

Pairing is an explicit later operation. Do not include the one-time code in an
argument, unit file, environment file, shell history, or documentation.

Create the code in the private VPS runtime only after the reviewed server commit
is deployed:

```bash
mcp-devbox edge pairing-create --state-root /state --ttl 10m
```

Then enter it through stdin on Parrot:

```bash
read -rsp 'One-time Edge pairing code: ' EDGE_PAIR_CODE; printf '\n'
printf '%s\n' "$EDGE_PAIR_CODE" | sudo -u mcpedge /usr/local/bin/mcp-edge pair \
  --server https://<REVIEWED_MCP_DEVBOX_ORIGIN> \
  --state /var/lib/mcp-devbox-edge \
  --name parrot-wsl-opencode
unset EDGE_PAIR_CODE
```

The private Ed25519 key remains local with mode `0600`. Verify that the state root
is `0700` and every regular identity/database file is `0600`:

```bash
sudo stat -c '%a %U:%G %n' /var/lib/mcp-devbox-edge
sudo find /var/lib/mcp-devbox-edge -maxdepth 2 -type f \
  -printf '%m %u:%g %p\n'
```

## 10. Install the systemd service

Create `/etc/systemd/system/mcp-devbox-opencode-edge.service`:

```ini
[Unit]
Description=MCP Devbox OpenCode Edge
After=network-online.target
Wants=network-online.target
ConditionPathExists=!/var/lib/mcp-devbox-edge/STOP

[Service]
Type=simple
User=mcpedge
Group=mcpedge
UMask=0077
RuntimeDirectory=mcp-devbox-edge
RuntimeDirectoryMode=0700
StateDirectory=mcp-devbox-edge
StateDirectoryMode=0700
WorkingDirectory=/var/lib/mcp-devbox-edge
ExecStartPre=/usr/bin/test -x /usr/bin/bwrap
ExecStartPre=/opt/mcp-devbox/opencode-1.18.1/opencode --version
ExecStart=/usr/local/bin/mcp-edge opencode \
  --state /var/lib/mcp-devbox-edge \
  --socket-root /run/mcp-devbox-edge \
  --opencode /opt/mcp-devbox/opencode-1.18.1/opencode \
  --driver /usr/local/libexec/mcp-devbox/model-turn-driver \
  --provider /opt/mcp-devbox/opencode-provider \
  --bubblewrap /usr/bin/bwrap \
  --integrity /opt/mcp-devbox/opencode-1.18.1/package-lock.json \
  --wait 120s \
  --poll 5s \
  --heartbeat 5s \
  --output-limit 1048576
Restart=on-failure
RestartSec=5s
TimeoutStopSec=20s
KillMode=control-group
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/mcp-devbox-edge /run/mcp-devbox-edge /srv/mcp-devbox-workspaces
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
RestrictNamespaces=false
LockPersonality=yes
RestrictRealtime=yes
RemoveIPC=yes
CapabilityBoundingSet=
AmbientCapabilities=
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

Install and inspect it, but start it only after pairing and workspace registration:

```bash
sudo systemd-analyze verify /etc/systemd/system/mcp-devbox-opencode-edge.service
sudo systemctl daemon-reload
sudo systemctl enable mcp-devbox-opencode-edge.service
sudo systemctl start mcp-devbox-opencode-edge.service
sudo systemctl status mcp-devbox-opencode-edge.service --no-pager
```

The unit has no inbound listener. Do not add a Windows port proxy or inbound
firewall rule.

## 11. Verify heartbeat, socket modes, and the first runtime

Inspect only bounded service metadata:

```bash
sudo journalctl -u mcp-devbox-opencode-edge.service --since -10m --no-pager
sudo find /run/mcp-devbox-edge -maxdepth 2 \
  -printf '%m %u:%g %y %p\n'
```

Each runtime directory must be `0700`; each model-turn socket must be `0600`.
There must be no TCP listener owned by `mcpedge`:

```bash
sudo ss -lntp
```

For the first reviewed runtime, verify on the server that heartbeats advance and
that completion contains four turns and the expected tools without duplicate
turns or duplicate consumption. Do not expose prompts, tool arguments, signatures,
tokens, IP addresses, or host paths in diagnostic output.

## 12. Cancellation and process-group cleanup

Server cancellation is observed through heartbeat. The launcher cancels the
runtime and kills the complete OpenCode process group. Verify after cancellation:

```bash
pgrep -a -u mcpedge 'opencode|model-turn-driver' || true
sudo find /run/mcp-devbox-edge -maxdepth 2 -type s -print
```

There must be no stale OpenCode process or runtime socket. A driver restart may
resume an awaiting turn from the journal; it must not create or consume the turn
a second time.

## 13. Kill switch and revocation

Stop new leases and cancel active work locally:

```bash
sudo -u mcpedge touch /var/lib/mcp-devbox-edge/STOP
sudo systemctl stop mcp-devbox-opencode-edge.service
```

Do not remove `STOP` until the incident or maintenance window is reviewed.
Revoke the device independently from the private VPS runtime:

```bash
mcp-devbox edge revoke --state-root /state --device ed_<OPAQUE_DEVICE_ID>
```

Revocation and the local kill switch are independent; use both when retiring or
compromising a device.

## 14. Update and rollback

An update is atomic and reviewed:

1. stop the unit;
2. set `STOP`;
3. build and test the reviewed commit;
4. install new Edge/driver binaries under temporary names;
5. install the pinned OpenCode binary, lockfile, and provider as one versioned set;
6. rerun the Bubblewrap preflight;
7. atomically rename the reviewed files into place;
8. remove `STOP` only after review;
9. start the unit and verify one runtime.

Keep the previous root-owned binaries and provider directory until the new runtime
passes. Roll back all four components together: Edge, driver, OpenCode/lockfile,
and provider. Do not roll back the private identity database unless restoring a
coordinated, known-good backup; stale journal state can otherwise cause duplicate
or ambiguous operations.

## 15. Uninstall

Revoke first, then stop and remove the local installation:

```bash
sudo -u mcpedge touch /var/lib/mcp-devbox-edge/STOP
sudo systemctl disable --now mcp-devbox-opencode-edge.service
sudo rm -f /etc/systemd/system/mcp-devbox-opencode-edge.service
sudo systemctl daemon-reload
sudo rm -f /usr/local/bin/mcp-edge
sudo rm -f /usr/local/libexec/mcp-devbox/model-turn-driver
sudo rm -rf /opt/mcp-devbox/opencode-1.18.1
sudo rm -rf /opt/mcp-devbox/opencode-provider
```

Delete `/var/lib/mcp-devbox-edge` only after revocation, audit retention, and any
required backup. Remove workspaces separately; uninstalling Edge must not silently
delete repositories.

## Explicitly out of scope

This guide does not install or grant:

- a Docker socket or membership in the Docker group;
- arbitrary sudo for `mcpedge`;
- remote-controlled `apt` or package installation;
- VPN, HTB, TryHackMe, or `/mnt/c`/`/mnt/d` access;
- Build Workcell or Goal Runtime;
- a browser profile, SSH key directory, or host home mount;
- a no-Bubblewrap fallback.
