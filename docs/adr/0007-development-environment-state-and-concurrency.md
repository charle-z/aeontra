# ADR 0007: Development environment state and concurrency

Status: Accepted

## Context

Development workspaces are mutable. Builds, formatters, package managers and tests create
ordinary source changes and generated files. Those changes are not evidence that the
workspace authority boundary has been crossed.

The original project resolver combined three different checks:

1. the durable project and workspace binding;
2. repository identity and host-path safety;
3. the current Git working-tree state.

It reported most failures as `project_checkout_unsafe`. Process control used that same
resolver, so a slow `git status` or a changed checkout could make an already-authorized
process impossible to inspect or stop. Each Edge also executed one durable control
operation at a time, which let a long build delay unrelated project status calls.

Toolchain state was stored below `.mcp-devbox` in the source checkout. That made runtime
provisioning visible to Git and coupled checkout inspection cost to cache size.

## Decision

### Project state

Project state is reported using separate meanings:

- `registered`: the durable project/workspace binding is available;
- `ready`: repository identity is valid and the Git tree is clean;
- `dirty`: repository identity is valid and the Git tree has normal changes;
- `unavailable` or `timeout`: an environmental inspection could not finish;
- `identity_mismatch`: the workspace or repository identity differs from its attestation;
- `corrupt`: required Git metadata is invalid;
- `unsafe_boundary`: ownership, containment or symlink policy was violated.

Only `unsafe_boundary` represents a security-boundary failure. Diagnostics expose a
stable reason, whether reconciliation is safe, and a bounded recommended action. They do
not expose host paths, command output or credentials.

The durable registry binding can be resolved without running Git. The registry listing
adds only a bounded repository-identity observation when an attestation is present; it
does not run status or discovery. New filesystem effects still validate the workspace
and repository boundary. Process reads and termination use the identity captured when
the process started and do not depend on a clean checkout.

The state vocabulary is intentionally finite. Durable registry state, derived
checkout observations, and durable operation state are separate concepts:

- registry claim state is persisted as `healthy`, `stale`, or `repairable`;
- checkout state is derived from a boundary and repository observation;
- operation state is persisted as `queued`, `leased`, `succeeded`, `failed`, or
  `cancelled`. `waiting_capacity`, `waiting_project`, `running`, `finalizing`,
  and `cancelling` are journal progress phases or cancellation flags, not
  additional durable terminal states.

The complete vocabulary is:

```text
Project:  registered | ready | dirty | unavailable | timeout |
          identity_mismatch | corrupt | unsafe_boundary
Registry claim: healthy | stale | repairable
Operation: queued | leased | succeeded | failed | cancelled
Operation phases: waiting_capacity | waiting_project | running | finalizing |
                  cancelling
Process:  starting | running | exited | stopping | stopped | killed | orphaned
Toolbox:  absent | creating | ready | stopped | stale | incompatible | repairable
```

The vocabulary describes observed state, not permission to bypass a failed check. A
missing or stale record can be reconciled only through its owner-bound recovery path.

### Source, runtime, cache and artifacts

Each registered workspace has four distinct roots:

```text
source      registered Git workspace
runtime     <edge-state>/project-runtime/<workspace-id>
cache       <edge-state>/project-cache/<workspace-id>
artifacts   <edge-state>/project-artifacts/<workspace-id>
```

The Edge creates the private roots, validates ownership and symlink safety, and mounts
only the exact roots into a Linux workcell or toolbox. Windows derives equivalent roots
from its configured Edge state directory; no drive letter is fixed by the product.

`HOME`, language package-manager caches and toolchain homes point at runtime or cache,
not source. Existing `.mcp-devbox` directories are not deleted automatically and remain
ordinary user data. New executions do not populate them.

### Operation scheduling

An Edge may lease and execute a bounded number of independent operations concurrently.
Project reads, process observation and work in different projects do not share a global
execution lock. Signed bundle update, rollback and repair remain Edge-wide exclusive
effects. Existing subsystem locks continue to protect their own journals and resources.

The durable operation lease ID is the completion fence. Completion validation and its
terminal write occur under the same store lock, so cancellation or lease recovery cannot
change the lease between those steps. Progress distinguishes pickup, running and
finalizing while terminal state remains durable.

### Toolbox and registry recovery

Toolbox records are versioned and bind workspace identity, mount policy, rootless engine
identity and a generation. Legacy records remain readable. A stopped, server-owned
toolbox with stale compatible metadata can be reconciled; an identity or boundary change
fails closed. Reconciliation never deletes the source workspace.

Repository claims can be listed and reconciled independently of Git discovery. A stale
claim may be released only after restating its repository identity. Healthy claims are
not released implicitly. A project row whose final target binding disappeared remains
visible under its preferred target and can be released by exact owner, repository and
generation, preventing a phantom alias conflict. Schema migrations preserve existing
projects, processes, operations and toolbox records.

## Security boundaries preserved

- The configured workspace roots and Edge state root remain administrator-controlled.
- Symlink, ownership, mount and repository-swap checks remain fail-closed.
- Workcells remain rootless and receive neither a host Docker socket nor arbitrary host
  filesystem access.
- Credentials remain outside workcells; publication still uses the Edge/server brokers
  and exact preview/execute contracts.
- Process IDs remain opaque and are checked against the captured OS identity to prevent
  PID reuse and cross-project control.
- Concurrency does not broaden an operation's authority.

## Consequences

Normal development writes no longer invalidate project observation or process control.
Toolchain caches no longer increase Git discovery cost. Independent projects can make
progress on one Edge while global maintenance stays exclusive.

Operators receive actionable state instead of an `unsafe` catch-all. Old records are
migrated or classified explicitly; recovery does not require resetting a repository,
deleting a workspace or reinstalling the Edge.
