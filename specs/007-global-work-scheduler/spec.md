# Spec — P16 Global Work Scheduler and Edge lifecycle

Status: **Step 0 — contract freeze candidate** on branch
`p16-global-work-scheduler`. No production worker, queue enforcement, host package,
or public-tool change exists until the staged implementation and exact-head gates
listed in `plan.md` pass.

## Goal

Make MCP Devbox the single normal admission and coordination point for expensive
work requested by any number of chats or agents while keeping the VPS responsive,
preserving independent Edge authority, and removing opaque workspace/runtime IDs
from normal user interaction.

P16 combines two inseparable product requirements:

1. durable scheduling, fairness, deduplication, resource enforcement, and separate
   VPS/Edge execution pools;
2. a reliable Edge lifecycle whose install, update, project discovery, reconnect,
   repair, and migration do not require repeated command sequences or manual ID
   transport.

A technically secure system that repeatedly asks the operator to recreate workspaces,
copy `ed_*`, `ws_*`, or `mr_*` values, repeat onboarding, or repair a partial update is
not acceptable.

## Measured VPS baseline

The production host was measured on 2026-07-22 with one idle capture, one real
Coolify redeploy without cache, and bounded `fio` probes. The authoritative summary is
`docs/baselines/2026-07-22-p16-capacity.md`.

Initial facts:

- KVM guest, 2 vCPU, AMD EPYC 7502 model exposed by the hypervisor;
- 3.805 GiB usable memory, approximately 77 GiB usable disk;
- idle CPU p95 approximately 41 percent of the two-vCPU host;
- build CPU p95 approximately 94 percent, maximum 100 percent;
- negligible CPU steal during the measured interval;
- build host-memory peak approximately 2.33 GiB with no OOM and negligible memory PSI;
- storage and monthly transfer were not the binding constraints;
- a real no-cache build lasted approximately 133 seconds and produced sustained CPU
  contention.

P16 therefore optimizes CPU admission and continuity first. Memory, I/O, and network
remain enforced dimensions but are not assumed to be current bottlenecks.

## User-experience invariants

Normal chat interaction uses human concepts:

```text
project: ekoparty-trip-agent
target: parrot
action: build
```

The user must not be required to supply these implementation identifiers in the normal
path:

```text
ed_...
ws_...
mr_...
et_...
job_...
lease IDs
idempotency keys
```

Those values remain available in bounded diagnostic and audit output.

Required behavior:

- a new chat can resolve and continue a registered project by alias;
- one Edge installation and pairing survives server redeploys, Edge restarts, signed
  updates, and ordinary WSL restarts;
- projects under an administrator-approved root are discovered, cloned, or associated
  without a manual `workspace add/configure/list` ritual;
- a missing workspace is recreated automatically only when doing so cannot lose local
  work; otherwise the system returns one precise recovery reason and action;
- a disconnected chat does not terminate an approved build;
- a disconnected Edge does not silently move work to the VPS;
- an update failure rolls back to the prior signed release instead of leaving a partial
  installation;
- error messages describe the stable cause and the next safe action, never only
  `not found`.

## Canonical Edge layout

P12/P15 names are milestone labels, not permanent user directories. The supported
layout is:

```text
~/.local/state/mcp-edge/
  identity/
  projects.db
  workspaces.db
  jobs.db
  results/

~/.cache/mcp-edge/
  builds/
  downloads/
  images/

~/workspaces/
  <development-project>/

~/htb-machines/
  <authorized-lab>/

/opt/mcp-devbox/releases/<SIGNED_RELEASE>/
/opt/mcp-devbox/current -> releases/<SIGNED_RELEASE>
```

Legacy state below `~/.config/mcp-devbox-edge`, known P12/P15 service units, and known
historical installation directories are migration inputs only. The installer must not
blindly rename or delete a directory named `p12`; it first classifies whether it is a
release artifact, repository, workspace, or unrelated directory.

## Installation and lifecycle contract

### Clean installation

The supported operator flow is at most:

```text
sudo apt install ./mcp-devbox-edge_<version>_amd64.deb
mcp-edge onboard --server <configured-server>
```

The signed package and guided onboarding perform all closed, deterministic setup:

- verify the signed bundle and architecture;
- detect supported Linux/WSL/systemd posture;
- create private state/cache/workspace roots;
- install and validate systemd units;
- enable and validate the user-owned rootless Podman or Docker endpoint;
- preserve or migrate existing identity and workspace state;
- pair only when no valid identity exists;
- assign a human alias such as `parrot`;
- run a bounded health smoke before declaring ready.

### Repeat installation and update

- package installation is idempotent;
- identity, project associations, contracts, checkpoints, jobs, and pending results are
  never regenerated or deleted by an update;
- schema migrations are transactional and versioned;
- activation uses an atomic release link;
- failed preflight, migration, service start, or health smoke restores the previous
  signed release and schema-compatible state;
- ordinary signed updates require no manual commands after initial installation;
- repair is a closed operation, not a shell.

### Doctor and repair

`mcp-edge doctor` returns only bounded checks and stable blocker codes.
`mcp-edge doctor --repair` may perform only reviewed repairs:

- recreate owned directories with fixed modes;
- migrate known legacy state;
- restore packaged units and compatibility links;
- enable/restart the fixed Edge and rootless-user services;
- restore a known signed release;
- remove only Edge-owned temporary files.

It may not delete repositories, change branches, accept commands/paths/URLs supplied by
an agent, or broaden filesystem/network authority.

## Project and workspace model

P16 separates four identities:

1. **Project** — durable human alias plus owner-bound repository identity.
2. **Workspace** — one validated physical checkout on one Edge or VPS execution root.
3. **Job** — durable requested effect with immutable target and resource profile.
4. **Attempt/runtime** — ephemeral executor attempt hidden from the normal interface.

The durable project registry binds:

```text
alias
repository owner/name
canonical clone identity
preferred target alias
workspace binding(s)
allowed profile(s)
local contract/checkpoint reference
```

A project alias does not grant new authority. It resolves only within administrator-
approved owners, roots, profiles, and devices.

### Automatic workspace resolution

For a repository `charle-z/ekoparty-trip-agent`, the default development path is:

```text
/home/<edge-user>/workspaces/ekoparty-trip-agent
```

Resolution order:

1. reuse an existing valid binding;
2. discover a matching safe checkout under an approved root;
3. associate a legacy path after local validation;
4. clone into the canonical path using the already configured Edge Git authority;
5. block when local uncommitted/untracked state could be lost or identity is ambiguous.

A path is rejected when it escapes the approved root, traverses a symlink, uses a
Windows mount for a Linux workcell, has unsafe ownership/modes, or points to a different
repository identity.

## Scheduler architecture

```text
N chats / agents
        |
        v
MCP Devbox control plane
  identity + policy + approval + durable scheduler
        |
        +-- pool vps.build
        +-- pool vps.deploy
        +-- pool edge.<device>.build
        +-- pool edge.<device>.runtime
```

The control plane is the normal admission authority. Executors apply an independent
local maximum and cannot be instructed to exceed it.

### Durable store

The initial scheduler uses SQLite under the private persistent `/state` volume:

```text
/state/workqueue/jobs.db
```

Required tables/records include jobs, dependencies, pools, leases, attempts, results,
deduplication keys, workspace fairness state, and migration metadata.

SQLite requirements:

- WAL mode where supported;
- foreign keys and explicit transactions;
- bounded database/page growth;
- one schema migrator;
- crash-safe lease recovery;
- no repository-controlled SQL or schema;
- backup with the existing `/state` lifecycle.

Redis is a non-goal for the first implementation. One control plane and low/moderate
job volume do not justify another resident network service, credential, backup path,
or memory reservation. The storage interface must remain replaceable if a future
multi-replica control plane requires distributed coordination.

### Job states

```text
planned
  -> queued
  -> dispatching
  -> running
  -> succeeded | failed | cancelled | blocked
```

Additional reason codes distinguish capacity, dependency, Edge-offline, revocation,
repair, result-pending-upload, and manual-review states without exposing secrets or
paths.

### Immutable execution target

Every approved job has exactly one target:

```text
kind: vps
```

or:

```text
kind: edge
device_id: ed_<opaque>
```

After approval:

- Edge work never falls back silently to VPS;
- VPS work never migrates to an Edge;
- changing target requires a new plan, new job, and new approval;
- a revoked or disconnected Edge blocks or resumes the same job according to policy;
- a job never inherits authority from another target.

No automatic fallback exists between VPS and Edge execution targets.

## Execution pools and fairness

Initial pools:

```text
vps.build                 max active: 1
vps.deploy                max active: 1
edge.<device>.build       local capability constrained
edge.<device>.runtime     local capability constrained
```

Edge capacity is per device. One Edge may support one heavy build, another two, and a
third more small builds, but only after local measurement and administrator policy.
The scheduler uses resource tokens, not only a numeric slot count.

Fairness is Deficit Round Robin by workspace/project using estimated cost. FIFO order
is retained within equivalent work. One project cannot fill or monopolize the global
queue.

Initial queue bounds:

```text
pending global: 64
pending per workspace: 8
active heavy per workspace: 1
```

All bounds are administrator configuration and have safe defaults and maximums.

## Resource model and equations

The mathematical layer optimizes only authorized work. It never changes identity,
target, network scope, credential access, approval, or local maxima.

For job `j` in pool `p`, dominant pressure is:

```text
D_j = max(
  CPU_j / CPU_p,
  RAM_j / RAM_p,
  IO_j / IO_p,
  PIDS_j / PIDS_p
)
```

Estimated cost is:

```text
C_j = D_j * estimated_duration_j + startup_cost_j
```

Hard admission requires every resource dimension and slot limit to fit:

```text
sum(active CPU) + CPU_j <= CPU_pool
sum(active RAM) + RAM_j <= RAM_pool
sum(active PIDS) + PIDS_j <= PIDS_pool
active_jobs < max_active
```

A bounded shadow priority score is:

```text
P_j = (
  w_u * approved_urgency
  + w_a * aging
  + w_b * downstream_unblock_value
  + w_k * cache_reuse_value
) / (epsilon + C_j)
```

Historical estimates update conservatively:

```text
estimate_next = (1 - lambda) * estimate_previous + lambda * observed
lambda default = 0.20
```

First release behavior:

- hard limits, fairness, queue bounds, deduplication, and aging are enforced;
- score components and recommended ordering run in shadow mode;
- weights remain administrator-owned and cannot be supplied by a repository, model, or
  chat;
- NaN, infinity, negative, overflow, and unbounded values are rejected;
- adaptation starts only after sufficient samples and remains clamped to profile
  minimum/maximum values.

## Deduplication

The deduplication identity includes at least:

```text
operation class
repository identity
exact commit
authorized build profile
relevant configuration digest
architecture
execution target
cache mode
```

Concurrent identical requests share one durable job and result. A mutable tag alone is
never an artifact identity; image deployment binds to a digest.

## VPS worker and builder boundary

The control-plane process does not compile. A separate worker consumes only reviewed
jobs from `vps.build`/`vps.deploy`.

The actual build engine must be rootless and separately resource constrained. Step 1
spikes rootless BuildKit first and rootless Podman as fallback. No design is declared
complete until the selected engine proves:

- no rootful Docker socket;
- bounded CPU/memory/PIDs and lower I/O priority;
- cancellation of the entire build process group;
- reusable cache;
- deterministic result identity;
- zero control-plane outage during the measured test.

Initial measured VPS policy candidate:

```text
max active build: 1
CPU default: 650 millicores
CPU calibration range: 500-800 millicores
MemoryHigh candidate: 1280 MiB
MemoryMax candidate: 1792 MiB
SwapMax candidate: 256 MiB
PIDs max: 512
I/O weight: 25
```

These are versioned defaults, not unchangeable constants. The isolated worker
calibration at 50/65/80 percent determines the production value.

## Edge continuity

A build is owned by the durable job and local Edge worker, not by a ChatGPT browser
session or one OpenCode runtime.

When the Edge loses the control-plane connection after starting an approved stage:

- it may finish only the already leased, locally revalidated stage;
- it cannot expand target, repository, network, registry, duration, resources, or
  credentials;
- it journals progress/result locally before execution/delivery transitions;
- it reconnects using the same job and attempt identity;
- a lost result upload is replayed without another build;
- a local `started` record is reconciled rather than blindly executed twice;
- expiration beyond the allowed offline grace produces a stable blocked/manual-review
  state, never automatic VPS fallback.

Initial policy candidates:

```text
lease TTL: 120s
offline grace: 10m
max build duration: 45m
pending result retention: 24h
```

## Configuration

All scheduler policy is documented and supplied through bounded administrator
configuration, primarily Coolify environment variables for the control plane/worker and
root-owned packaged configuration on Edge. An environment value declares policy;
cgroups/rootless engine controls enforce it.

Initial names:

```text
MCP_DEVBOX_WORKQUEUE_DB
MCP_DEVBOX_QUEUE_MAX_PENDING_GLOBAL
MCP_DEVBOX_QUEUE_MAX_PENDING_PER_WORKSPACE
MCP_DEVBOX_QUEUE_JOB_TTL
MCP_DEVBOX_VPS_BUILD_MAX_ACTIVE
MCP_DEVBOX_VPS_BUILD_DEFAULT_CPU_MILLIS
MCP_DEVBOX_VPS_BUILD_DEFAULT_MEMORY_MIB
MCP_DEVBOX_WORKER_POOL_ID
MCP_DEVBOX_WORKER_CPU_MIN_MILLIS
MCP_DEVBOX_WORKER_CPU_DEFAULT_MILLIS
MCP_DEVBOX_WORKER_CPU_MAX_MILLIS
MCP_DEVBOX_WORKER_MEMORY_HIGH_MIB
MCP_DEVBOX_WORKER_MEMORY_MAX_MIB
MCP_DEVBOX_WORKER_SWAP_MAX_MIB
MCP_DEVBOX_WORKER_PIDS_MAX
MCP_DEVBOX_WORKER_IO_WEIGHT
MCP_EDGE_BUILD_LEASE_TTL
MCP_EDGE_BUILD_OFFLINE_GRACE
MCP_EDGE_BUILD_MAX_DURATION
MCP_EDGE_BUILD_RESULT_RETENTION
```

Every value has a parser, lower/upper bounds, safe default, startup validation, docs,
and adversarial tests. Cgroups/rootless controls enforce the configured policy. The local executor applies the minimum of requested resources
and its own maximum.

## Platform and image flow

P16 preserves VPS builds as a supported development/production workflow. GitHub
Actions remains optional.

The later image path is:

```text
build@vps or build@edge
  -> push image when requested and authorized
  -> immutable image digest
  -> deploy@vps through Coolify
  -> health verification
```

Git source credentials, Edge registry-push credentials, and Coolify registry-pull
credentials are separate authorities. No secret enters repository content, tool output,
or chat.

Existing `platform_deploy*` tool inputs retain their semantics. Queue integration is
additive: execution returns a durable job reference/state and eventually the Coolify
deployment identity. Auto-deploy/webhook paths for scheduler-managed applications must
be disabled or detected; manual root/Coolify intervention is documented as audited
break-glass, not normal operation.

## Public interface direction

High-level tools operate on project aliases and intent:

```text
project_resolve
project_status
project_continue
project_build_preview
project_build
work_job_status
work_job_cancel
work_queue_status
```

Low-level ID tools remain for diagnostics and compatibility but are not the normal
path. Existing tool names and schemas are not removed or silently repurposed.

## Observability

Safe metrics include:

- queue depth and wait p50/p95;
- active jobs by pool;
- duration and attempts;
- deduplication/cache hits;
- CPU usage/throttling/PSI;
- memory current/peak/events;
- I/O bytes/pressure;
- health latency and 502 count;
- Edge online/offline/result-pending state;
- fairness/starvation indicators.

Metrics and logs exclude prompts, commands, secrets, raw paths, private targets, and
repository content.

## Acceptance

P16 cannot close until all are proven:

- clean Edge installation requires at most two operator commands;
- repeat install/update preserves identity, projects, workspaces, jobs, and results;
- P12/P15 state migrates without re-pairing when valid;
- a new chat continues a project by alias without opaque IDs;
- safe workspace discovery/clone/association works without manual registration;
- a missing workspace is automatically recovered only when lossless;
- 20 concurrent VPS build requests produce at most one active VPS build;
- concurrent identical requests produce one build;
- control-plane health stays available with zero observed 502 during calibration;
- an Edge disconnect during build does not lose or duplicate the job;
- reconnect uploads the same result;
- revoked/disconnected Edge work never falls back to VPS;
- each Edge enforces its own independent local resource maximum;
- restart of control plane and worker recovers queued/leased work safely;
- resource limits are visible in configuration and enforced by cgroups/rootless engine;
- all existing public tool contracts retain compatibility;
- exact-head remote gates, dated baseline, production smoke, rollback evidence, and tag
  complete before closure.

## Non-goals

- No Kubernetes, autoscaling cluster, or multi-tenant scheduler.
- No Redis or external queue in the first implementation.
- No arbitrary shell, Docker root socket, repository-defined profile, or model-defined
  resource limit.
- No automatic Edge-to-VPS or VPS-to-Edge fallback.
- No automatic deletion/move of unknown legacy directories.
- No promise that one Edge supports multiple builds until measured and configured.
- No requirement to use GitHub Actions for normal development builds.
- No claim of final optimal CPU quota before isolated 50/65/80 calibration.
