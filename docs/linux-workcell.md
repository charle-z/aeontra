# Trusted Linux Workcell

Status: merged and deployed on 2026-07-18. Pairing, rootless Podman, Bubblewrap, systemd, OpenCode 1.18.1, provider loading, and one real six-sequence remote repository smoke were validated on the owner's Parrot WSL2 machine. The canonical installation procedure is `docs/install-opencode-edge-parrot.md`.
The operator workflow for authorized HTB rooms is `docs/htb-lab-workflow.md`.

## One profile, two local contexts

P12 adds exactly one opt-in local profile:

```text
linux-workcell
├── dev        (default)
└── htb-linux  (optional local workspace context)
```

`htb-linux` is metadata of `linux-workcell`, not a second execution profile. The VPS continues to receive only an opaque `workspace_id`, the bounded goal, timeout, runtime identifiers, states, digests, and safe metrics. Host paths, machine metadata, VPN interface, target address, LHOST, flags, credentials, loot, and full tool output remain local to Edge and the selected workspace.

The profile is displayed as:

```text
TRUSTED LINUX WORKCELL
network posture: trusted_host_shared_network
```

That wording is intentional. This version shares the host network. It does not implement target-only network isolation, overlay networking, nftables filtering, or general egress filtering.

## Architecture

```text
ChatGPT
  │ opaque workspace_id + goal + timeout ≤ 3600 s
  ▼
MCP Devbox VPS
  │ signed outbound Edge lease; no host path or HTB metadata
  ▼
mcp-edge on Parrot WSL
  ├── local trusted workspace registry
  │   ├── profile: linux-workcell
  │   ├── mode: dev | htb-linux
  │   └── optional HTB metadata
  ├── local preflight
  │   ├── canonical Linux path and no symlink
  │   ├── allowed root
  │   ├── tun interface + IPv4 + target route for htb-linux
  │   ├── rendered immutable instructions
  │   └── sanitized local tool inventory
  ├── model-turn-driver over a private Unix socket
  └── Bubblewrap
      ├── host network explicitly shared
      ├── selected workspace read/write
      ├── private runtime read/write
      ├── system runtime and tools read-only
      ├── optional user-owned rootless container socket
      └── OpenCode
```

The trusted registry is administered by `mcp-edge`. Model output, repository files, and the VPS cannot modify profile, mode, path, target, VPN interface, or other local authority metadata. Edge resolves the opaque ID and revalidates the complete local runtime contract before and after preflight; any drift fails closed.

## `sandbox` versus `linux-workcell`

| Property | Existing `sandbox` | `linux-workcell` |
| --- | --- | --- |
| Opt-in | Existing workspace registration | Explicit `--profile linux-workcell` |
| Network | No network; `--unshare-all` remains isolated | `--unshare-all --share-net`; host network intentionally shared |
| Filesystem | One workspace and private runtime | Same boundary, plus workspace-local package prefixes and optional read-only wordlists |
| Docker | No socket | Optional user-owned rootless Docker or Podman socket only |
| Rootful Docker | Rejected | Rejected |
| Package installation | Not a general development environment | User-scoped prefixes and rootless containers |
| HTB instructions | None | Optional rendered `htb-linux` context |
| Host paths on VPS | Never | Never |
| Sudo | No | No general sudo |
| Windows mounts | Rejected | Rejected |

The old sandbox validator explicitly rejects `--share-net`. Linux Workcell has a separate validator and cannot silently weaken the sandbox.

## Allowed workspace roots

Default roots are resolved from the local Edge user's home:

```text
$HOME/workspaces/<PROJECT>
$HOME/htb-machines/<MACHINE>
```

For the owner's planned Parrot account this becomes:

```text
/home/charles/workspaces/<PROJECT>
/home/charles/htb-machines/<MACHINE>
```

Rejected paths include:

- `/mnt/c` and `/mnt/d`;
- a root directory itself instead of a child workspace;
- symlinks or paths traversing symlinked parents;
- another registered workspace;
- the rest of the Edge user's home;
- SSH keys, browser profiles, Edge identity/state, and private control-plane sockets.

## Local CLI

Register a development workspace with explicit trust:

```bash
mcp-edge workspace add \
  --profile linux-workcell \
  /home/charles/workspaces/example
```

The command returns an opaque ID. Development mode is automatic:

```bash
mcp-edge workspace configure <workspace_id> --mode dev
```

Register an authorized HTB Linux workspace and configure its local context:

```bash
mcp-edge workspace add \
  --profile linux-workcell \
  /home/charles/htb-machines/Paperwork

mcp-edge workspace configure <workspace_id> \
  --mode htb-linux \
  --machine Paperwork \
  --target 10.10.11.250 \
  --difficulty EASY \
  --os LINUX \
  --vpn-interface tun0
```

Inspect sanitized local tool availability without printing executable or workspace paths:

```bash
mcp-edge workspace inventory <workspace_id>
```

List or remove registrations:

```bash
mcp-edge workspace list
mcp-edge workspace remove --id <workspace_id>
```

All metadata remains in the local SQLite registry. A legacy workspace row is migrated to `sandbox` and `dev`; it is never silently promoted to `linux-workcell`.

## Runtime filesystem

Every Linux Workcell creates these private paths idempotently:

```text
<workspace>/.mcp-devbox/
├── instructions.md       # rendered per runtime, mode 0400
├── current-state.md      # durable checkpoint, mode 0600, bounded to 1 MiB
├── tool-inventory.json   # sanitized local inventory, mode 0400
├── tools/                # user-scoped package/tool prefixes
├── cache/                # package caches
└── runtime/              # temporary runtime-owned files
```

HTB mode additionally creates:

```text
scans/
loot/
scripts/
reports/
tmp/
tickets/
```

Directories are private (`0700`). Atomic replacement rejects symlinked or unsafe parents and targets.

## User-scoped dependencies

The workcell environment points package managers to the workspace:

```text
PATH=<workspace>/.mcp-devbox/tools/bin:...
XDG_CACHE_HOME=<workspace>/.mcp-devbox/cache
PIP_CACHE_DIR=<workspace>/.mcp-devbox/cache/pip
npm_config_cache=<workspace>/.mcp-devbox/cache/npm
PNPM_HOME=<workspace>/.mcp-devbox/tools/bin
PIPX_HOME=<workspace>/.mcp-devbox/tools/pipx
PIPX_BIN_DIR=<workspace>/.mcp-devbox/tools/bin
GOPATH=<workspace>/.mcp-devbox/tools/go
GOBIN=<workspace>/.mcp-devbox/tools/bin
CARGO_HOME=<workspace>/.mcp-devbox/tools/cargo
RUSTUP_HOME=<workspace>/.mcp-devbox/tools/rustup
TMPDIR=<workspace>/.mcp-devbox/runtime/tmp
```

This supports virtualenv/pip, pipx, npm/pnpm, `go install`, Cargo, downloads, compilers, child processes, and temporary services without general sudo. Host `apt` dependencies remain an owner-controlled setup action or a future exact local approval profile.

## Local tool inventory

The inventory checks a bounded, validated PATH without a shell. Each entry contains only:

```json
{
  "name": "nmap",
  "available": true,
  "version": "7.95",
  "capability": "network-recon"
}
```

Versions are time-bounded, output-bounded, and reduced to a safe version token. Executable paths are never returned. The catalog covers common development and Parrot tooling, including Python, Go, Node/npm/pnpm, Rust/Cargo, gcc, nmap, curl, wget, OpenSSL, content discovery tools, SMB/LDAP clients, Impacket, NetExec, password-auditing tools, and Docker/Podman when present. Missing tools are reported as `absent`; the runtime does not assume every Parrot package exists.

## Rootless Docker or Podman

Linux Workcell never mounts:

```text
/var/run/docker.sock
/run/docker.sock
```

Edge may expose one endpoint only when all checks pass:

- Edge is running as a non-root user;
- the engine CLI is from an allowlisted system/tool root;
- the Unix socket is under `/run/user/<edge_uid>`;
- the socket and its parent are not symlinks;
- the socket is owned by the Edge user;
- the parent is not world-writable;
- the socket is not accessible to other users.

The socket is mounted at the private namespace path:

```text
/runtime/rootless-container.sock
```

The runtime receives:

```text
DOCKER_HOST=unix:///runtime/rootless-container.sock
CONTAINER_HOST=unix:///runtime/rootless-container.sock
MCP_DEVBOX_CONTAINER_ENGINE=docker|podman
MCP_DEVBOX_CONTAINER_LABEL=mcp.devbox.runtime=<runtime_id>
COMPOSE_PROJECT_NAME=<runtime-derived-name>
```

Every container, network, and volume created by the task must carry the exact runtime label. After OpenCode exits or is cancelled, Edge lists resources only through that exact label, validates each returned identifier, and removes them with fixed engine arguments. A cleanup error is recorded as pending and fails the local runtime rather than claiming success.

The socket is powerful within the rootless user's namespace. Rootless does not mean harmless: a compromised model can still consume local CPU, memory, disk, and network until limits, cancellation, or owner intervention stop it.

## Development mode

`dev` is the default and adds no hacking instructions. The goal may ask OpenCode to:

- inspect and modify the selected repository;
- install dependencies into workspace-local prefixes;
- run checks, tests, builds, browsers, and temporary services;
- build and run rootless containers;
- start PostgreSQL or a Chromium smoke environment;
- debug and experiment inside the selected workspace.

OpenCode must read `instructions.md` and an existing `current-state.md` before acting. It validates actual repository, process, service, and test state instead of blindly trusting the checkpoint.

When a local GitHub authority is explicitly configured, `dev` also receives the
private owner-bound clone and planned publication actions described in
`docs/development-edge-git.md`. The credential remains outside the namespace. GitHub
Actions, PR and check inspection use the public MCP GitHub API tools instead.

## HTB Linux context

Before starting `htb-linux`, Edge validates locally:

1. the workspace is below the configured HTB root;
2. `target_ip` is one IPv4 address, not CIDR;
3. the configured VPN interface exists;
4. that interface has IPv4;
5. `ip route get <target>` reports that exact interface;
6. LHOST is derived from that interface.

The VPN is started outside Bubblewrap and remains controlled by the owner. A failed preflight writes no runtime instructions and starts no model.

The versioned template is `profiles/htb-linux-v1.md`. It preserves authorization, user/root goals, bounded recon, anti-loop rules, guided enumeration, exploitation, lateral movement, privilege escalation, flag handling, cleanup, response format, the confirmed-chain model, and newly-published-machine behavior. It renders only local values and forbids writeups, flags, solutions, spoilers, or machine-specific external hints.

This version does **not** technically restrict shared host networking to the single target. The target contract is operational and locally validated, not an nftables or overlay enforcement boundary.

## Durable local state

`instructions.md` is an immutable rendered contract for one runtime. It records mode, goal, local workspace references, evidence locations, package prefixes, and HTB values when applicable. It contains no device secret.

`current-state.md` remains writable and local. Development checkpoints summarize objective, changes, tests, active processes, blocker, and next action. HTB checkpoints summarize phase, access, locally obtained credentials, user/root flag status, confirmed findings, discarded branches, active processes, artifacts, cleanup, and one next action.

At runtime termination Edge appends a bounded checkpoint with:

- completed, cancelled, timed out, or failed state;
- rootless container cleanup result;
- stopped runtime process-group status;
- exact local checkpoint path.

Large scans, build logs, loot, scripts, and evidence stay in their normal workspace directories. The VPS does not retain them.

## Cancellation and limits

The lease timeout remains bounded to at most 3600 seconds. OpenCode runs in a new process group with Bubblewrap `--die-with-parent` and `--new-session`; cancellation terminates the runtime group. Rootless cleanup commands are independently time- and output-bounded.

This version deliberately does not add Goal Runtime, 24-hour jobs, a scheduler, durable remote jobs, Windows access, Active Directory, an overlay network, or nftables target filtering. A human-operated Parrot onboarding flow is now packaged and documented; it does not automate pairing codes, sudo, or remote trust decisions.

Direct foreground `project_exec` and durable `project_process_start/status/stop/signal/list/cleanup` are a
separate GPT Web execution path from the legacy OpenCode runtime. Both direct paths use
the trusted-workcell Bubblewrap construction, workspace-local writable state and
host-shared network posture. Background processes receive an opaque durable identity,
private redacted logs and explicit TERM/KILL lifecycle; ending the MCP turn does not
stop them. OpenCode remains installed as an optional fallback and is not launched by
these tools.

The direct path also supports a persistent rootless toolbox per registered development
workspace. The toolbox keeps its writable Debian rootfs, installed packages and caches
across calls and Edge restarts, mounts the selected workspace at `/workspace` and the
already validated user-owned Podman/Docker endpoint at one fixed private socket path.
It never mounts a rootful socket or modifies the host WSL package database. Creation, status, arbitrary
argv execution, installation, explicit repair and cleanup are available without
starting an OpenCode/model runtime. Named background services use the same toolbox;
their opaque identities survive chat and Edge-daemon restarts while the container is
running. Service status never starts a stopped container, and service argv/environment
are deliberately not persisted for automatic replay after a WSL/container restart.
Creation also binds configurable CPU, memory and process limits to the persistent
private record. Omitted values use broad server defaults. Each later operation compares
those values with the engine's live `HostConfig`; drift or a request to reuse the same
workspace with different limits fails closed. Storage continues to be the rootless
user's persistent container storage; status reports bounded writable/rootfs byte usage
and storage is removed only by explicit toolbox cleanup. Installed remote Podman,
Docker or Compose clients inherit server-owned endpoint variables that callers cannot
override.

## Verification matrix

Local automated coverage includes:

- backwards-compatible registry migration;
- explicit profile opt-in and strict roots;
- typed dev/HTB configuration;
- HTB VPN/route fixture and fail-closed paths;
- template rendering and no unresolved placeholders;
- private directories and atomic state files;
- resume without overwriting completed state;
- unchanged sandbox rejection of shared network;
- Linux Workcell selection from the local registry;
- exact mount allowlisting and rejection of unexpected host data;
- sanitized tool inventory and no local path leakage;
- rootless socket ownership/path validation;
- rootful Docker rejection;
- runtime label cleanup and unsafe engine-output rejection;
- terminal state persistence;
- complete Go suite, vet, and build.

Race remains a GitHub Actions gate when the local public runner has CGO disabled. Real PostgreSQL, Chromium, rootless-engine, and VPN behavior must be validated in the private Parrot/validation environment; CI uses controlled fixtures rather than a public HTB machine.

## Residual risks

- Host-shared networking permits general outbound traffic and callbacks. There is no target isolation yet.
- A rootless container engine still grants broad authority within that user's namespace.
- Runtime labels require the task to label every created resource; unlabeled resources cannot be identified safely by a label-only cleanup routine.
- Tool versions and behavior depend on the reviewed Parrot installation.
- The model can alter any file in the selected workspace, including its local evidence and checkpoint.
- Resource ceilings beyond the existing lease/process controls need host/systemd and rootless-engine configuration.
- HTB no-writeup behavior is an operational contract, not an Internet-content filter.

These risks are explicit. The profile is trusted and personal, not a replacement for the fail-closed sandbox.

## Exact Parrot setup after merge and deployment

Do not perform these steps until the exact P12 merge commit is deployed and the required GitHub checks are green.

### 1. Confirm the Linux roots

As the future Edge user `charles`:

```bash
install -d -m 0700 /home/charles/workspaces
install -d -m 0700 /home/charles/htb-machines
install -d -m 0700 /home/charles/.local/state/mcp-edge
```

Verify none are symlinks or Windows mounts:

```bash
findmnt -T /home/charles/workspaces
findmnt -T /home/charles/htb-machines
namei -l /home/charles/workspaces /home/charles/htb-machines
```

### 2. Install owner-controlled host dependencies

Use interactive local sudo only for setup:

```bash
sudo apt-get update
sudo apt-get install --yes --no-install-recommends \
  bubblewrap ca-certificates git golang-go nodejs npm python3 python3-venv \
  pipx build-essential pkg-config iproute2 curl wget openssl
```

Install optional Parrot tools and wordlists through the distro packages you trust. The running model receives no sudo.

### 3. Install a rootless engine if desired

Choose Docker rootless or Podman rootless. Do not add `charles` to a rootful `docker` group and do not expose `/var/run/docker.sock`.

For Podman, verify the user socket:

```bash
systemctl --user enable --now podman.socket
systemctl --user status podman.socket --no-pager
stat -c '%U %G %a %F %n' "$XDG_RUNTIME_DIR/podman/podman.sock"
podman --url "unix://$XDG_RUNTIME_DIR/podman/podman.sock" info
```

For Docker rootless, use the vendor/distro-supported rootless installation, then verify:

```bash
systemctl --user status docker --no-pager
stat -c '%U %G %a %F %n' "$XDG_RUNTIME_DIR/docker.sock"
docker --host "unix://$XDG_RUNTIME_DIR/docker.sock" info
```

Stop if the socket is outside `/run/user/$(id -u)`, symlinked, root-owned, or accessible to other users.

### 4. Build the exact reviewed commit

```bash
git clone https://github.com/charle-z/mcp-devbox.git /tmp/mcp-devbox-p12
cd /tmp/mcp-devbox-p12
git checkout <EXACT_P12_MERGE_COMMIT>
git status --short
go test ./...
go build -trimpath -o /tmp/mcp-edge ./cmd/mcp-edge
sudo install -o root -g root -m 0755 /tmp/mcp-edge /usr/local/bin/mcp-edge
```

Install the reviewed driver, provider, pinned OpenCode binary, integrity file, and systemd service using the P11.2 Parrot guide. Do not replace the pinned OpenCode/provider pair independently.

### 5. Register a development workspace

```bash
git clone <REPOSITORY_URL> /home/charles/workspaces/<PROJECT>
mcp-edge workspace add \
  --profile linux-workcell \
  /home/charles/workspaces/<PROJECT>
mcp-edge workspace list
mcp-edge workspace inventory <workspace_id>
```

Confirm mode `dev`, network posture `trusted_host_shared_network`, no printed executable paths in inventory, and no Windows path.

### 6. Register an HTB workspace only when authorized

Create the local directory before registration:

```bash
install -d -m 0700 /home/charles/htb-machines/<MACHINE>
mcp-edge workspace add \
  --profile linux-workcell \
  /home/charles/htb-machines/<MACHINE>
mcp-edge workspace configure <workspace_id> \
  --mode htb-linux \
  --machine <MACHINE> \
  --target <TARGET_IP> \
  --difficulty <EASY|MEDIUM|HARD> \
  --os LINUX \
  --vpn-interface tun0
```

Start the VPN manually outside the runtime and validate:

```bash
ip -4 address show dev tun0
ip route get <TARGET_IP>
```

The route must report `dev tun0`. Edge will repeat this check before starting.

### 7. Run private smokes before real work

Use a disposable development repository and a controlled local HTB fixture. Verify:

- repository edit and tests;
- workspace-local Python/Node dependency;
- rootless image build;
- temporary PostgreSQL;
- Chromium smoke;
- cancellation and process-group stop;
- container/network/volume cleanup by exact runtime label;
- `current-state.md` after completion and cancellation;
- HTB template render, fixture user/root flags, cleanup, and resume without repeated recon.

Do not use a public HTB machine as CI evidence. After those smokes pass, keep the owner-controlled VPN and kill switch available during real authorized rooms.

### 8. Doctor local

Antes de emparejar o ejecutar trabajo real, verifica usuario, roots, Bubblewrap, binarios, OpenCode, provider, inventario y endpoint rootless. Ejecuta id; namei -l sobre ambos roots; bwrap --version; mcp-edge --help; model-turn-driver --help; opencode --version; workspace list; y el status del socket rootless. Detente si Edge corre como root, un root es symlink o mount Windows, Bubblewrap falla, OpenCode no es 1.18.1, o el socket no está bajo /run/user/<uid>.

### 9. Pairing

Genera un código de un solo uso desde el terminal privado con mcp-devbox edge pairing-create --state-root /state --ttl 10m. En Parrot léelo sin guardarlo en historial y pásalo por stdin a mcp-edge pair con el servidor HTTPS, el state root /home/charles/.local/state/mcp-edge y el nombre parrot-trusted-linux. Confirma el dispositivo con mcp-devbox edge devices --state-root /state.

### 10. Primer smoke

Usa un repositorio desechable en modo dev. El smoke debe editar un archivo, ejecutar tests, instalar una dependencia de usuario, iniciar y detener un servicio temporal, construir una imagen rootless, ejecutar PostgreSQL y Chromium, cancelar un runtime y confirmar cleanup completo por label. No uses un proyecto personal ni una máquina HTB pública.

### 11. Cancelación

Crea /home/charles/.local/state/mcp-edge/STOP y detén mcp-devbox-edge.service. Confirma que el process group murió, que no quedan containers, networks ni volumes con el runtime label y que current-state.md registra la cancelación. Elimina STOP solo tras revisar el estado.

### 12. Reanudación

Retira STOP, inicia el servicio y lee current-state.md antes del siguiente lease. Revalida procesos, servicios, filesystem, pruebas y acceso real. No repitas recon, instalaciones o ramas descartadas sin una variable material nueva.

### 13. Update

Actualiza únicamente a un merge commit exacto ya desplegado y con checks verdes. Haz fetch, checkout del SHA exacto, ejecuta la suite, compila con trimpath, instala el binario root-owned y reinicia el servicio. Driver, provider y OpenCode se actualizan como un conjunto revisado cuando sus pins cambien.

### 14. Rollback

Conserva hashes y binarios anteriores. Detén el servicio, reinstala el binario del SHA previo desde /opt/mcp-devbox/rollback y vuelve a iniciar. No borres registro local, workspaces ni memoria durante un rollback binario.

### 15. Revocación

Revoca primero desde producción con mcp-devbox edge revoke --state-root /state --device ed_<opaque>. Después detén el servicio local. La revocación debe impedir nuevos leases aunque el estado local permanezca.

### 16. Desinstalación

Revoca antes de borrar identidad. Deshabilita y detén el servicio, elimina su unidad, los binarios instalados, provider, OpenCode y el state root local. Los repositorios y evidencias bajo /home/charles/workspaces y /home/charles/htb-machines se conservan hasta una revisión humana separada.
