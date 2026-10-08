# L3 Sandbox Plan (historical)

Status: **Superseded by ADR 0005 and the private-runner implementation candidate.**

This file records the earlier Docker exploration. It is not current deployment
guidance. The accepted boundary is `public MCP -> authenticated private runner ->
rootless Podman`; the public MCP never receives a container-engine socket. See
`docs/adr/0005-separated-l3-and-native-windows-execution.md`, `docs/security.md`, and
`docs/runbooks/private-sandbox-runner.md`.

## Goal

Let ChatGPT run broader build commands without turning the public MCP endpoint into
a free terminal. L3 exists so commands can be more capable while still being
confined by OS isolation, default-deny egress, and audit.

## Non-Negotiable Requirements

- no free terminal before L3
- human approval remains required for risky command execution in `ask` mode
- default-deny egress for sandboxed commands
- block metadata and private networks by default, including `169.254.169.254` and
  RFC1918 ranges
- preserve the app-level policy gate: jail, secret deny, command allowlist, redaction,
  and audit still run before a command reaches the sandbox
- no Docker socket in the public MCP container
- grants remain local-human-only and are not part of the sandbox API

## Decision

The public `mcp-devbox` HTTP container must not own host-level sandbox power. In
particular, do not mount `/var/run/docker.sock` into the internet-facing MCP
container just to let it run `docker run`; Docker socket access is effectively
host-root and would make the MCP daemon a high-value escape target.

Instead, L3 is a separate runner boundary:

1. `mcp-devbox` keeps exposing MCP and policy.
2. A local, host-controlled sandbox runner owns the privileged isolation mechanism
   (Docker/gVisor/nsjail/bubblewrap depending on the host).
3. The daemon talks to the runner through an explicit runner contract, not a shell.
4. The runner receives only: workspace path, argv, environment allowlist, network
   profile, timeout, and resource limits.
5. The runner returns only: exit code, stdout/stderr, duration, and sandbox metadata.

That contract lets us add broad build commands later without giving the model a
general-purpose host control plane.

## Runner Contract

The explicit runner contract is:

```text
Run(ctx, request) -> result

request:
  cwd: exact workspace path already resolved by Policy
  argv: explicit program + args, never a shell string
  env: allowlisted environment variables only
  network: none | allowlist
  allow_egress: explicit hostnames/CIDRs when network=allowlist
  timeout: bounded duration
  resources: cpu/memory/pids/filesize limits

result:
  exit_code
  stdout
  stderr
  duration
  sandbox_backend
  egress_profile
```

The daemon must still redact stdout/stderr before returning output to the agent.

## Backend Order

Preferred backend order:

1. **Host-managed gVisor/Docker runner** on VPS/Linux. Good isolation if configured
   on the host, not by mounting Docker socket into the public MCP container.
2. **nsjail/bubblewrap runner** when the host allows required namespaces/capabilities.
3. **Plain exec** remains L1 only and must not be described as L3.

Coolify note: the current app container is non-root and capable enough for Go/git,
but it is not an OS sandbox for child processes. Treat it as L1/L2 app policy, not
as L3.

## Egress Policy

Default network profile is `none`. Build tools that need network must declare an
egress allowlist such as:

- `github.com`
- `proxy.golang.org`
- `sum.golang.org`
- selected package registries for the repo

Always block:

- `169.254.169.254`
- RFC1918 ranges
- loopback addresses other than the runner's own control channel
- link-local and multicast ranges

## Implementation Steps

1. Add a `SandboxRunner` interface beside the current `Runner`, plus tests that
   prove command tools cannot opt into broad execution unless a sandbox backend is
   configured.
2. Add a `sandbox_status` diagnostic or startup log that clearly says whether L3 is
   active. If inactive, broad/free commands must remain unavailable.
3. Implement the first Linux runner backend behind an explicit config flag/env var.
4. Add adversarial tests for workspace escape, secret exfil through permitted
   commands, network egress to metadata/RFC1918, timeout, and resource limits.
5. Only after those tests pass, consider a broader command tool. Until then,
   `run_command` remains allowlist-only and audited.

## Status on branch `l3-sandbox` (2026-07-02)

- **Docker backend implemented + unit-tested** (`internal/tools/sandbox_docker.go`):
  `dockerArgv` builds a hardened `docker run` (`--network none` = egress deny,
  `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges`, non-root
  `--user`, `--pids-limit`/`--memory`/`--cpus`, `/tmp` tmpfs, only the workspace
  bind-mounted). Tests lock these flags and assert it NEVER emits `--privileged`,
  the docker socket, `--network host`, `--cap-add`, host pid/userns, or `-v /:`.
- Wired via config (`MCP_DEVBOX_SANDBOX=docker`, image via `MCP_DEVBOX_SANDBOX_IMAGE`,
  default `golang:1.26-alpine`). `nsjail`/`gvisor` remain "pending".
- **IMPORTANT — not yet live for execution:** the runner currently backs
  `sandbox_status` only. No tool routes command execution through it, and
  `run_command`/`run_tests` stay L1 allowlist-only. Enabling broad execution is gated
  behind step 4 (adversarial escape/egress/timeout/limit tests) run on Linux/WSL2.
- **Containment VERIFIED on Linux+Docker (WSL2, 2026-07-02).** The integration tests
  in `sandbox_docker_adversarial_test.go` all pass against a real Docker daemon:
  baseline run works; egress to `169.254.169.254` and the internet is DENIED
  (`--network none`); host files outside the workspace are not readable; the read-only
  rootfs blocks writes; and a hung command is killed by the timeout. Run them with
  `go test ./internal/tools/ -run Integration -v` on a Linux host with Docker.
- With containment proven, step 5 is unblocked: a broad-execution tool may route
  commands through the sandbox (see `sandbox_exec`).

## Where to run the sandbox (VPS vs PC) + easy config

Two deployment shapes, both driven by the same env config:

### A. On the VPS (primary)
The public MCP container is socketless by design, so it cannot itself run
`docker run`. Options, in order of preference:
1. **Run the whole app container under a sandboxed runtime** (gVisor/`runsc`, or a
   locked-down Docker runtime with a default-deny egress network policy) at the
   Coolify/host level. Then in-container execution is already OS-isolated — set
   `MCP_DEVBOX_SANDBOX=gvisor` (once that backend lands) for accurate status. No DinD,
   no socket.
2. **Run the daemon on the VPS host** (not in the socketless container) where Docker
   is available, with `MCP_DEVBOX_SANDBOX=docker`. The daemon spawns hardened
   ephemeral containers per command.
Never mount `/var/run/docker.sock` into the internet-facing container.

### B. Interact with the PC (when the VPS is too small, or you want local repos)
Keep the PC unexposed; front it with the VPS you already trust:
1. Daemon runs on the **PC**, bound to `127.0.0.1`, `MCP_DEVBOX_SANDBOX=docker`
   (Docker Desktop / WSL2), `--mode ask`, roots limited to chosen repos.
2. **Reverse SSH tunnel to the VPS** (reuses the existing domain + Traefik TLS):
   `ssh -R 8766:127.0.0.1:8765 user@vps`, and route a hostname
   (e.g. `pc.<domain>`) to that port. No inbound ports opened on the home network.
3. ChatGPT points at the clean `https://pc.<domain>/mcp` endpoint with OAuth. Same bearer
   auth + policy + the PC's local Docker sandbox contains execution.
4. Result: the VPS provides the secure public front + TLS; the PC provides compute
   and local repos; commands are sandboxed locally; nothing on the PC is exposed
   except through the authenticated tunnel.

### Easy-config summary (env only)
```
MCP_DEVBOX_SANDBOX=docker|gvisor|nsjail|none   # default none
MCP_DEVBOX_SANDBOX_IMAGE=golang:1.26-alpine    # docker backend image
MCP_DEVBOX_MODE=ask                            # approvals for risky actions
MCP_DEVBOX_ROOT=/repos/<repo>                  # (VPS) or the PC repo path
```
Switching host (VPS↔PC) is: run the daemon there, set these envs, point the tunnel/
connector at it. No code change.
