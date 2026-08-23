# ADR 0005 — Separate L3 and native Windows execution boundaries

Status: **Accepted for staged implementation**

Date: 2026-08-23

## Context

Aeontra currently has three related but distinct execution surfaces:

- Layer 1 commands in the public MCP process, constrained by repository jail and
  command allowlist;
- trusted Linux workcells on a paired outbound Edge;
- fixed-profile execution in private services such as the validation runner.

The public `sandbox_exec` contract exists, but production has no verified private L3
executor. The Docker implementation can currently report itself available without a
verified endpoint, and the public MCP container intentionally has no container-engine
socket. The Edge protocol, workspace registry, direct execution, durable processes,
onboarding and signed bundle lifecycle are Linux-specific.

Two defects must be fixed before either surface receives more authority:

1. an unknown `MCP_DEVBOX_MODE` value can reach policy switches whose default branch
   behaves as `allow`;
2. an Edge cancellation may be acknowledged after its bounded wait while the effect
   goroutine or child process remains alive. Concurrent process admission is also not
   serialized.

The desired additions are:

- arbitrary argv and explicitly requested shells inside a real L3 boundary on the VPS;
- a native Windows Edge target that runs Win32 processes inside registered Windows
  workspaces;
- incremental stdin for durable processes on Linux and Windows.

L3 and Windows Edge are not the same trust boundary. L3 protects the VPS. A Windows
workcell executes on a separately paired device under its local administrator policy.

## Decision

Implement the work as ordered, reviewable layers. Each layer must fail closed when its
executor or platform contract is unavailable.

```text
AI client
   |
   v
public MCP control plane
   |-- Layer 1 policy and allowlisted argv
   |-- durable queue, leases, audit and redaction
   |
   |-- private L3 request ------------------------------+
   |                                                    |
   |                                             private VPS executor
   |                                             rootless containers
   |                                             network default deny
   |
   +-- signed outbound Edge operation -----------------+
                                                        |
                                  +---------------------+--------------------+
                                  |                                          |
                         trusted Linux workcell                    trusted Windows workcell
                         Bubblewrap/process groups                 Win32/Job Objects/ACLs
```

### 1. Policy mode is an exhaustive type

Only `read-only`, `ask` and `allow` are valid server modes. Empty input normalizes to
`read-only`. Configuration and direct policy construction reject every other value.
All policy switches handle `allow` explicitly and return an error for an unknown value.

Workspace modes such as `dev` and `htb-linux` remain separate Edge metadata even though
they currently reuse the `MCP_DEVBOX_MODE` environment name inside a workcell.

### 2. Lifecycle correctness precedes new authority

Cancellation is successful only after the effect has terminated or a platform-specific
reconciliation has recorded a bounded, controllable identity. A timeout cannot turn a
still-running effect into an acknowledged cancellation.

Process admission is serialized and later connected to the existing P16 execution
pools. Direct workcells receive administrator-owned CPU, memory, process and wall-clock
limits. Linux uses cgroup-v2 enforcement where the host supports the reviewed profile;
Windows uses Job Objects. Chat disconnection alone never implies cancellation, but an
explicit cancellation, expired authority or local kill switch must converge without
leaving an unowned process.

### 3. L3 uses a private rootless executor

The public MCP container keeps the `SandboxRunner` client contract and never receives a
Docker, Podman or BuildKit socket. A separate private executor owns one validated
user-scoped rootless Podman endpoint. BuildKit remains available for image builds but is
not the general argv executor.

The control request contains only:

- a server-owned workspace identity;
- relative working directory;
- argv;
- a sanitized environment map;
- fixed network profile;
- bounded timeout, CPU, memory, process and output limits;
- an idempotency identity and request digest.

The executor maps the workspace identity to an administrator-configured root. It never
accepts a host path, socket, image, runtime, mount or network namespace from MCP input.
It launches an ephemeral non-root container with:

- network `none` by default;
- read-only rootfs and private bounded temporary storage;
- only the selected workspace writable;
- all capabilities dropped and no new privileges;
- no host PID, IPC, devices, home, credential stores or infrastructure sockets;
- bounded output and deterministic cleanup after exit, failure or timeout.

The executor image is administrator-owned and pinned by digest. A sandbox workspace is
eligible only after a no-follow preflight confirms that policy-denied secret paths are
absent or masked by the reviewed executor profile. Output redaction remains defense in
depth, not the secret boundary.

`sandbox_status` reports `available: true` and `free_terminal: true` only after the
private executor proves its rootless endpoint, image identity and containment profile.
An unavailable or drifting executor leaves `sandbox_exec` unavailable. There is no host
execution fallback.

`run_command` remains Layer 1 and continues using `MCP_DEVBOX_ALLOW_CMD`.
`sandbox_exec` does not consult that allowlist. Shell syntax works only when the caller
explicitly selects a shell argv such as `bash -lc`.

### 4. Windows is a native trusted workcell

Add the administrator-enabled `windows-workcell` profile with `dev` mode and the
explicit network posture `trusted_windows_host_shared_network`. It reuses device
identity, outbound transport, leases, heartbeat, cancellation, journals, aliases,
pairing and revocation. It does not create another protocol or Front Door.

The target runs under a dedicated non-administrator Windows identity when available.
The first release is documented as a trusted workcell, not an AppContainer sandbox.
It receives only:

- registered workspace roots;
- private Edge runtime and temporary directories;
- installed development executables;
- an explicit sanitized environment.

Personal profile directories, browser profiles, credential stores, SSH agents and
infrastructure credentials are outside the service account ACL.

Windows path validation uses handles rather than string-prefix checks:

- open with reparse-point-aware flags;
- resolve the final handle path and volume identity;
- compare case-insensitively beneath one configured local-volume root;
- reject UNC/device paths, alternate data streams and unsupported reparse tags;
- reject junctions, symlinks and path replacement during registration and execution;
- validate the dedicated account's ACL boundary.

Linux root handling and `/mnt` rejection remain unchanged.

Foreground and durable Windows execution use explicit argv and a Windows Job Object.
The job owns the full child tree, sets kill-on-close and administrator resource limits,
rejects breakaway, and records process creation time with the opaque process identity.
No public contract accepts a PID, Job handle or host path.

Windows onboarding and updates use the Service Control Manager with a dedicated account
as the recommended production posture. A user-level Task Scheduler installation may be
offered only as a clearly weaker development profile. Signed Windows bundles bind
platform and architecture and use immutable versioned directories plus an atomic active
marker; they do not emulate Unix `current` symlinks with junctions.

### 5. Durable stdin is an ordered process capability

Add `project_process_stdin` for a process already owned by one project and target. The
public request contains:

- alias and target;
- opaque process id;
- idempotency key;
- expected byte offset;
- bounded UTF-8 data;
- optional explicit EOF.

The idempotency record binds process id, offset, data digest and EOF. Retries return the
same receipt; changed content, stale offsets, closed stdin, terminal processes and wrong
project/target ownership fail closed. Results contain only accepted byte count, next
offset and closed state. Input content is excluded from audit and observability.

The process worker owns the actual pipe. Linux uses a private framed Unix socket beneath
owner-only Edge state. Windows uses a named pipe with a service-account-only ACL. Both
enforce frame limits, write deadlines, serialization, backpressure, process identity and
an idempotent EOF transition. Stop and write races must converge without input crossing
between processes.

On Linux the client verifies Unix peer credentials against the exact recorded worker PID
and Edge UID before sending a frame. The worker retains the last committed frame receipt
for exact replay across an Edge manager restart. The worker and child share one lifecycle,
so a worker replacement cannot inherit a live child after losing that receipt. A queued
write remains cancellable, but a leased write is non-cancellable and must persist the
exact accepted prefix. Backpressure closes the channel and returns that prefix instead of
turning a committed write into an ambiguous retry. Frames are limited to 32 KiB and the
complete ordered input stream, including the optional start payload, is limited to 16 MiB
per process.

The owner-only state directory is not mounted into the Bubblewrap workcell. Linux file
permissions cannot distinguish two arbitrary processes already running as the Edge OS
account, so that account remains part of the trusted host boundary. Untrusted project
code receives neither the state directory nor the stdin socket path.

## Threat model

| Threat | Required control |
|---|---|
| Unknown policy mode grants writes | Exhaustive validation in config and policy; no permissive default |
| Public MCP escapes through container socket | No engine socket or privileged runner in the public container |
| L3 reads host or service secrets | Server-owned workspace mapping, no home/state mounts, secret preflight/masking |
| L3 reaches metadata/private services | Network namespace disabled by default; no caller-selected network |
| Fork/memory/output denial of service | Rootless container limits, bounded output, timeout and pool admission |
| Cancellation leaves work alive | Effect-specific reconciliation before cancellation acknowledgement |
| Duplicate lease repeats an effect | Idempotency digest, fence and durable terminal receipt |
| Windows path escapes workspace | Handle-based final path, volume, reparse and ACL validation |
| Windows child escapes ownership | Job Object, no breakaway, creation-time identity and kill-on-close |
| Stdin is duplicated or crosses processes | Offset, digest, idempotency, private framed channel and ownership checks |
| Credentials leak through environment | Constructed environment; no ambient user/service credential inheritance |

## Implementation sequence

1. Reject unknown server modes and remove permissive switch defaults.
2. Fix cancellation reconciliation and concurrent process admission; add bounded direct
   process resource contracts.
3. Extract the common durable process manager and add ordered incremental stdin on
   Linux.
4. Add the private rootless L3 executor and real Linux/VPS containment acceptance.
5. Add Windows workspace/path/ACL primitives.
6. Add Windows foreground and durable Job Object execution, including stdin.
7. Add Windows project/Git integration, onboarding, lifecycle, doctor and signed bundle.
8. Run Windows CI and real Windows Edge installation/pair/update/rollback acceptance.

Assets, automated chat handoff and the future semantic supervisor remain separate later
work. Resource reconciliation and admission from steps 2–3 must land before those
features create additional workloads.

## Validation gates

Linux and common gates:

- focused RED/GREEN tests;
- complete Go suite and relevant race tests;
- vet, build, formatting, Staticcheck and diff checks;
- L3 escape, egress, secret, timeout, cleanup and resource tests;
- exact-head container and security gates;
- real target-VPS rootless acceptance.

Windows gates:

- `GOOS=windows` builds for every affected command;
- Windows GitHub Actions tests;
- real NTFS junction/reparse/ACL and quoting tests;
- native `cmd.exe` and PowerShell execution;
- Job Object process-tree timeout, stop and cleanup;
- incremental stdin/stdout/stderr with `codex app-server`;
- clean install, pair, reconnect, signed update and rollback on a real Windows Edge.

Cross-compilation alone never marks the Windows target ready.

## Consequences

The design adds one private VPS executor and a Windows-specific platform layer. This is
more code and operational surface than extending `run_command`, but it preserves the
existing authority boundaries and avoids exposing the host engine to the public MCP
process.

The first Windows profile is intentionally a trusted workcell. Stronger AppContainer or
restricted-token isolation may be added after measured compatibility testing, without
misrepresenting the initial filesystem boundary.

The staged sequence permits focused pull requests and rollback at each boundary. Product
documentation must continue reporting source, deployed backend, signed release and real
device acceptance as separate facts.
