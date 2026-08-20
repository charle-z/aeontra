# Install the outbound development Edge in dedicated WSL

Do not execute this procedure until the Step 5–7 branch has been reviewed,
published, deployed to the VPS, and `/healthz` confirms that exact commit. These
are human installation steps; mcp-devbox does not create a distro, use remote
sudo, or pair itself.

## Resulting boundary

- A dedicated Ubuntu WSL2 distribution, not Parrot and not the user's general
  development distro.
- A dedicated unprivileged `mcpedge` account.
- Workspaces only below `/srv/mcp-devbox-workspaces`; never `/mnt/c` or `/mnt/d`.
- Private identity and execution journal below `/var/lib/mcp-devbox-edge`.
- Outbound HTTPS from WSL to the existing VPS only at the control-plane layer.
- Untrusted project commands inside Bubblewrap with a private mount namespace,
  no network namespace connectivity, no home, no Edge state, and no Docker socket.
- A local `STOP` file that prevents new leases and cancels active work.

The first workcell supports only the structured `validate` objective and locally
detects Go, locked pnpm/npm, Python, or Rust validation stages. The VPS cannot
supply an argv array or shell text. `registry` is reserved in the protocol but the
initial workcell rejects it because a registry-only egress boundary is not yet
implemented.

## 1. Create the dedicated distro

From an elevated PowerShell terminal, inspect the distributions offered by the
installed WSL version and install a current Ubuntu LTS as a separate distro:

```powershell
wsl --list --online
wsl --install --distribution Ubuntu-24.04 --name MCP-Devbox-Edge
```

If that WSL version does not support `--name`, install/import a separate Ubuntu
distro through the supported WSL UI/command first; do not reuse Parrot. Confirm
the final name with `wsl --list --verbose`, then enter it:

```powershell
wsl --distribution MCP-Devbox-Edge
```

## 2. Install the reviewed binary and isolation dependency

These `sudo` calls are local, interactive provisioning by the owner. The running
Edge service never receives or invokes sudo.

```bash
sudo apt-get update
sudo apt-get install --yes bubblewrap ca-certificates git golang-go
sudo useradd --create-home --shell /bin/bash mcpedge
sudo install -d -o mcpedge -g mcpedge -m 0700 /var/lib/mcp-devbox-edge
sudo install -d -o mcpedge -g mcpedge -m 0700 /srv/mcp-devbox-workspaces
git clone https://github.com/charle-z/aeontra.git /tmp/mcp-devbox-edge-src
cd /tmp/mcp-devbox-edge-src
git checkout <REVIEWED_COMMIT>
go test ./internal/edge ./internal/edgeclient ./cmd/mcp-edge
go build -trimpath -o /tmp/mcp-edge ./cmd/mcp-edge
sudo install -o root -g root -m 0755 /tmp/mcp-edge /usr/local/bin/mcp-edge
```

Before continuing, confirm that unprivileged Bubblewrap works. This must print
`isolated` and return zero:

```bash
BWRAP_RUNTIME=(--ro-bind /usr /usr)
for path in /bin /lib /lib64 /etc; do
  [ ! -e "$path" ] || BWRAP_RUNTIME+=(--ro-bind "$path" "$path")
done
sudo -u mcpedge bwrap --unshare-all "${BWRAP_RUNTIME[@]}" \
  --proc /proc --dev /dev --tmpfs /tmp -- /usr/bin/printf 'isolated\n'
```

If this fails, stop. Do not weaken the service or run Edge as root.

## 3. Create and consume one pairing code

In the private Coolify terminal of the deployed mcp-devbox container:

```bash
mcp-devbox edge pairing-create --state-root /state --ttl 10m
```

Back in the dedicated WSL terminal, enter that code without placing it in shell
history or process arguments:

```bash
read -rsp 'One-time Edge pairing code: ' EDGE_PAIR_CODE; printf '\n'
printf '%s\n' "$EDGE_PAIR_CODE" | sudo -u mcpedge /usr/local/bin/mcp-edge pair \
  --server https://mcp-devbox-charlez.duckdns.org \
  --state /var/lib/mcp-devbox-edge \
  --name wsl-development
unset EDGE_PAIR_CODE
```

The client creates its Ed25519 private key locally with mode `0600`; only the
public key crosses the outbound connection. Pairing output contains the opaque
device id. Verify the server now lists exactly that active device:

```bash
mcp-devbox edge devices --state-root /state
```

## 4. Add one workspace without Windows mounts

Clone or copy a repository as `mcpedge` directly into the Linux filesystem:

```bash
sudo -u mcpedge git clone <REPOSITORY_URL> /srv/mcp-devbox-workspaces/<WORKSPACE_NAME>
```

Do not symlink a directory from `/mnt/c` or `/mnt/d`. Edge rejects the root and
workspace if it encounters those mounts or symlinks.

## 5. Install but do not silently weaken the service

Copy the reviewed unit from
`packaging/systemd/mcp-devbox-edge.service`, then start it:

```bash
sudo install -o root -g root -m 0644 packaging/systemd/mcp-devbox-edge.service /etc/systemd/system/mcp-devbox-edge.service
sudo systemctl daemon-reload
sudo systemctl enable --now mcp-devbox-edge.service
sudo systemctl status mcp-devbox-edge.service --no-pager
```

The unit exposes no listening port. It needs no inbound Windows or VPS firewall
rule.

## Kill switch and revocation

Stop new work and cancel an active execution locally:

```bash
sudo -u mcpedge touch /var/lib/mcp-devbox-edge/STOP
sudo systemctl stop mcp-devbox-edge.service
```

Remove `STOP` manually only after reviewing the situation. Independently revoke
the device from the private VPS terminal:

```bash
mcp-devbox edge revoke --state-root /state --device ed_<opaque>
```

Revocation invalidates all later signed requests from that device. Delete the
local distro or identity only after revocation and any required audit review.
