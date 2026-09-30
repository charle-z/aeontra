# ADR 0008 — Development-complete capability resolution

Status: **Proposed**

Date: 2026-09-30

## Context

Aeontra already has several governed development execution surfaces: the networkless L3
sandbox, trusted Linux workcells, persistent project toolboxes and services, rootless
container access, browser automation, fixed privileged profiles, validation runners,
durable project tasks, exact-base worktrees, leases, fences, model-turn state and
acceptance receipts.

Those mechanisms are useful but are selected separately. A repository can appear
toolchain-compatible while its documented workflow still requires properties outside the
chosen environment. The BuildKit #7206 validation exposed this gap: Go and Make were
available, and rootless Docker plus workspace bind translation were later available, but
the complete `make validate-all` workflow also exercised nested user namespaces,
uid/gid mappings, delegated cgroups, containerd/stargz workers, Git metadata and GitHub
Actions runtime/cache contracts.

Treating every such failure as a new tool-specific exception would create an unbounded
profile catalog and encourage authority to leak into ordinary workcells.

The product goal is therefore stronger than dependency bootstrap. A supported repository
workflow should state what it needs; Aeontra should select or provision the least-authority
environment that satisfies those requirements and continue the same durable objective.

## Decision

Introduce a generic development capability model above the existing execution surfaces.

The first source slice is implemented in `internal/development`. It is deliberately an
internal kernel and grants no new execution authority by itself.

### Capability requirements

A workflow step owns a canonical set of namespaced capability requirements such as:

```text
toolchain.go
build.make
container.docker.rootless
filesystem.workspace-bind
git.metadata.full
namespace.user.nested
idmap.subuid
cgroup.v2.delegated
ci.github-actions.runtime
ci.github-actions.cache
service.containerd
worker.stargz
```

A repository, workflow parser or failure classifier may identify a requirement. That fact
does not grant the capability. Unknown requirements remain unsatisfied until a
server-owned execution environment attests them.

Capability IDs are intentionally generic and tool-independent. Production routing must
not branch on a repository name such as BuildKit.

### Environment attestations

Every execution environment that participates in resolution has a server-owned
attestation containing:

- stable environment identity;
- closed execution class;
- monotonically changing generation;
- canonical capability set;
- deterministic attestation digest.

The digest changes when the environment generation or capability set changes. Callers
cannot enlarge an attestation by supplying repository data.

Initial execution classes follow the existing authority ladder:

1. workcell;
2. managed toolchain;
3. persistent toolbox;
4. dedicated rootless runtime;
5. brokered privileged capability;
6. isolated or external validation runner.

These are authority classes, not command allowlists.

### Policy envelope

Each objective owns an explicit resolution policy describing the allowed execution
classes and maximum authority tier. The resolver may choose only environments inside that
envelope.

Availability alone never authorizes a higher tier.

The resolver chooses the lowest-authority *single* environment that satisfies every
requirement. It never combines partial authority from multiple environments into one
attempt.

### Immutable execution attempts

ADR 0004 made a scheduled target immutable. That invariant remains correct at the
execution-attempt level.

Development-complete refines the higher-level model:

```text
objective
  -> step
      -> attempt A: immutable source + immutable environment attestation
      -> attempt B: new identity after a classified failure
```

An existing attempt is never retargeted. Capability migration creates a new attempt with
a parent identity.

This preserves auditability while allowing the objective to continue on a more suitable
governed runner.

### Source identity

An attempt is bound to both an environment attestation digest and a source-content
digest.

Continuation rules are closed:

- `code_failure`: source must change and environment authority must remain unchanged;
- capability/environment failures: environment attestation must change;
- external transient failure: exact source and environment may be retried;
- policy denial: stop;
- reconciliation-required: reconcile before another effect.

This prevents a code assertion failure from triggering privilege escalation and prevents
a missing-capability failure from replaying forever on an unchanged environment.

### Requirement refinement

A failure may reveal a requirement that was not known during initial planning. An
objective may add requirements to a failed or planned step, but cannot silently remove
requirements or change its authority policy.

This allows a workflow to begin with known properties such as Go and Make, observe a
typed nested-user-namespace failure, add that requirement, resolve a compatible runner
and continue. The original attempt and evidence remain immutable.

### Failure classification

The initial closed classes are:

```text
code_failure
capability_missing
capability_drift
dependency_missing
service_unavailable
kernel_semantics_missing
ci_contract_missing
filesystem_mapping_missing
platform_mismatch
external_transient
policy_denied
reconciliation_required
```

The class determines continuation posture. Free-form stderr may inform diagnosis but
must not directly grant authority or select privileged execution.

### Semantic acceptance remains separate

A succeeded execution attempt moves an objective only to acceptance pending. Runtime
completion, exit zero, a Git receipt or a test receipt is evidence; none alone proves the
natural-language objective.

The existing project-task acceptance separation remains the model to preserve.

## Security invariants

Development-complete does not mean a host shell.

This ADR does not authorize:

- rootful Docker sockets in workcells;
- general sudo or host filesystem access;
- caller-provided privileged argv;
- repository-controlled runner policy;
- implicit provider, credential or data-location changes;
- mutation of an already leased target;
- retry of consequential effects after uncertain acknowledgement.

Bubblewrap and current workspace boundaries remain unchanged.

Higher authority is supplied by a dedicated governed execution class with the smallest
required capability set. A privileged broker should provision an environment or one
narrow capability, not execute arbitrary repository-selected commands as root.

Logical workspace/source/artifact handles should be translated server-side when a daemon
or isolated runner needs another filesystem namespace. Hidden host paths stay hidden.

## BuildKit #7206 acceptance fixture

The internal regression fixture models the real capability classes observed while
running the documented BuildKit pre-submit workflow.

It proves:

- Go plus Make do not imply `make validate-all` compatibility;
- rootless Docker plus workspace bind translation still do not satisfy nested
  userns/cgroup/GHA/stargz requirements;
- the resolver selects an isolated runner only when the objective policy permits it;
- without one environment satisfying all requirements, resolution fails rather than
  merging authority across runners;
- a kernel capability failure can refine requirements and create a new immutable attempt
  without rewriting the original attempt.

This fixture is acceptance evidence for the generic resolver. It is not a production
BuildKit special case.

## Delivery after this ADR

The remaining implementation sequence is:

1. persist objectives, step requirements, attempts and capability receipts in the
   existing durable coordination plane rather than introducing a second scheduler;
2. build attestations from L3, workcell, toolbox, rootless runtime and runner state;
3. connect dependency/service provisioning to capability receipts and generations;
4. add a generic isolated runner/VM broker for kernel and CI contracts;
5. classify execution failures into the closed failure vocabulary;
6. bind command/test evidence to exact source and environment digests;
7. connect model-provider continuity to the same durable objective;
8. execute BuildKit `make validate-all` as an end-to-end release gate.

Source implementation, deployment, signed Edge release and real-device acceptance remain
separate facts.

## Consequences

Positive:

- environment selection becomes generic instead of tool-specific;
- authority escalation is explicit, bounded and auditable;
- failed capability retries cannot loop on an unchanged environment;
- source changes and environment changes are independently visible;
- existing workqueue, Edge, toolbox and task primitives can remain the execution plane.

Costs:

- every execution class needs trustworthy capability attestation;
- objective persistence needs a versioned migration and restart tests;
- failure classification must avoid treating code failures as environment failures;
- isolated runner lifecycle and artifact/source transfer become first-class security
  boundaries;
- old documentation that describes target immutability at the whole-objective level must
  be read as attempt immutability after this ADR is accepted.
