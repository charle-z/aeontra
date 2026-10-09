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

Exact numeric toolchain pins are represented generically as cumulative capability
prefixes. For example, an observed Go `1.26.6` can attest `toolchain.go.v1`,
`toolchain.go.v1-26` and `toolchain.go.v1-26-6`; a repository requiring Go `1.26`
therefore cannot be satisfied by an environment that only attests `v1-25`. Broad
constraints are never degraded to an unversioned claim: when the existing detector has
already proved the fixed L3 baseline satisfies the range, the requirement is bound to
that concrete baseline version; an Edge-required range that still needs version
selection fails closed until the managed version resolver can satisfy it.

Numeric capability versions preserve their precision. `1.95` denotes the
minor-version prefix, while `1.95.0` requires that exact patch. Dropping trailing
zero components would incorrectly allow `1.95.1` to satisfy the latter.

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

The source implementation now has adapters for four existing typed states:

- authenticated L3 status must retain its exact rootless, network-deny, filesystem, Git
  and core-toolchain posture;
- a trusted development workcell uses only its sanitized local inventory plus fixed
  Bubblewrap/workspace/network properties;
- a Codex rootless runtime is a separate higher-authority attestation, and Docker
  client/Buildx capabilities are added only after the signed container-client bundle
  passes its existing filesystem validation;
- a running toolbox must retain its generation, fixed base image and non-zero
  CPU/memory/PID limits; the Debian image grants package/service provisioning but never
  implies that Java, pnpm or another project toolchain is already installed.

Repository toolchain detection is converted only into requirements. It never adds a
capability to one of those environment attestations.

The confined Go version probe uses `GOTOOLCHAIN=local` to measure the installed
executable without downloading the module-selected toolchain. This override is
limited to inventory; normal command execution and managed provisioning retain
their existing toolchain selection semantics.

Initial execution classes follow the existing authority ladder:

1. networkless L3 sandbox;
2. trusted workcell;
3. managed toolchain;
4. persistent toolbox;
5. dedicated rootless runtime;
6. brokered privileged capability;
7. isolated or external validation runner.

L3 and the trusted workcell are intentionally distinct. A networkless L3 execution must
not inherit host-shared networking merely because a workcell is available.

These are authority classes, not command allowlists.

### Policy envelope

Each objective owns an explicit resolution policy describing the allowed execution
classes and maximum authority tier. The resolver may choose only environments inside that
envelope.

Availability alone never authorizes a higher tier.

The resolver chooses the lowest-authority *single* environment that satisfies every
requirement. It never combines partial authority from multiple environments into one
attempt.

### Durable objective scope

A development objective may carry one immutable project/target scope. The scope uses the
same bounded human aliases as project resolution and is serialized inside the canonical
objective record.

Legacy version-1 objective records without a scope remain readable for inspection and
backup compatibility, but the development supervisor refuses to dispatch them. Once a
scoped objective is created, its project or target cannot change in a later revision.

The supervisor therefore never accepts a caller-selected execution target for an
attempt. It obtains the project/Edge binding from the durable objective scope and obtains
candidate execution environments from server-owned attestation providers.

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

### Internal durable supervisor

The source now includes an internal supervisor above the existing workqueue store. It is
not a public MCP authority surface.

For one scoped objective step it:

1. reloads the current durable revision;
2. requests a bounded environment catalog from injected server-owned attestation
   providers;
3. resolves the least-authority compatible environment;
4. derives the attempt identity from objective, step, parent attempt, source digest,
   requirements and environment attestation;
5. persists the planned revision through workqueue CAS;
6. re-fetches the catalog before moving the attempt to `running`.

If the selected environment changed between plan and start, the supervisor records the
planned attempt as `capability_drift` instead of executing it. If requirements were
refined and the planned environment no longer satisfies them, it records
`capability_missing`. Both cases can be automatically replanned into a new child attempt
within a hard bounded transition budget.

Identical concurrent planning converges on one deterministic attempt. Divergent planning
requests cannot fork one objective revision. Code-failure retries stay on the exact
previous environment and require changed source; external-transient retries stay on the
same source and environment even if a newly available lower-authority environment
appears.

A composite catalog source combines at most 16 providers and at most 64 environment
attestations. Provider failure, invalid attestation, or duplicate environment identity
fails the whole catalog closed.

### Durable provisioning transport

An unsatisfied step can select one bounded plan from registered server-owned
provisioners, within the objective's immutable authority ceiling. The plan binds the
provider, queue pool, profile, promised capabilities and optional base attestation;
it contains no executable, host path, source body or credential.

The objective stores that plan before enqueueing one deterministically keyed job.
Retries recover the same job through workqueue idempotency. A worker persists its
current fence before asking the provider to reconcile the stable effect ID. The
provider must recover pending effects and reject stale authority before starting a
new effect. Each coordinator round has a bounded deadline; long effects remain in
the provider's existing journal rather than holding a reconciliation call open.

A completion receipt records only the effect outcome. Capability readiness requires
a fresh compatible environment attestation; semantic acceptance remains separate.
Cancellation recovers lost enqueue acknowledgements and can stop a captured leased
effect even after the objective itself becomes terminal. The transport is internal:
concrete provisioners and execution dispatch must be wired before claiming live
automatic provisioning.

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

#### P6: command contract and acceptance evidence

Registered Linux source inspection uses its own fingerprint domain. The selected
source is the union of Git HEAD paths, index paths and non-ignored untracked paths,
not a recursive filesystem scan. Fixed limits are 32,768 unique paths, 2 MiB per Git
pathname list, 4096 bytes per relative path or leaf-link text, and 64 MiB of cumulative
regular content and link text. Leaf symlinks contribute their text without target
resolution or reads; root/parent symlinks, directories, gitlinks and special files remain
unsupported. Descriptor-relative reads, entry identity checks, registry revalidation
and stable before/after HEAD/status evidence still apply. Source Git capture has a
separate bounded stdout budget and rejects truncation instead of parsing a partial list.
The stricter managed-worktree fingerprint and its existing domain/limits are unchanged.
Previously pinned command evidence must match its original digest; a different source
fingerprint does not grant permission to rebind or replay it.

A command-only objective may bind each step to a command contract containing
the canonical argv digest, source digest, staged private-body reference and digest,
required capabilities, and opaque artifact references. The durable objective stores no
argv, source bytes, command output, URL, or artifact path. The contract digest also binds
the objective and step identity plus the immutable resolution policy. A command
acceptance objective contracts every step; it cannot mix command-only acceptance with
natural-language evaluator steps.

Planning rejects a source digest that differs from the contract. The attempt captures the
selected environment attestation. A terminal zero-exit result leaves the objective in
acceptance pending until an internal dispatcher verifies the exact successful Edge
operation, device, operation kind and request key, workspace, source, environment and
fence. The dispatcher then submits a receipt bound to the objective, step, attempt,
contract, command, source, environment, staged-body reference and artifact references.
The core checks these bindings and stores the receipt in the same immutable revision that
moves the objective to accepted; the existing objective-record digest and revision CAS
protect persistence and concurrent updates.

The core receipt value is not a cryptographic attestation. Receipt authenticity remains
the responsibility of the server-side dispatcher before it calls the internal acceptance
method; no caller-facing receipt input is exposed. Natural-language objectives remain
acceptance pending until an explicit evaluator accepts them. Legacy records without a
command contract remain readable and do not gain command acceptance authority during
parsing.

An isolated GitHub execution uses its real numeric run ID, durable effect ID and
broker receipt digest instead of an invented Edge operation ID. These receipts
are valid only for isolated-runner attempts. Edge receipts remain bound to the
original Edge operation and cannot carry GitHub-run identity fields. The dispatcher
authenticates the source of either receipt; the core checks their immutable command,
source, environment and private-body bindings.

The command contract is immutable for the lifetime of its objective. After a code
failure, a source change requires a new objective with a newly staged private body and
contract; this slice does not support in-place contract refinement.

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

## Delivery status

The source persists bounded canonical objective/step/attempt records inside the
existing workqueue SQLite store with revision CAS, transition validation and
v2-to-v3 migration. Capability adapters, exact numeric toolchain requirements,
durable provisioning and command acceptance are implemented.

`project_development_start`, `project_development_status` and
`project_development_cancel` connect exact commands to the existing coordinator.
`project_development_list` recovers forgotten request IDs from the same journal,
scoped to project, target and current device identity. Its bounded newest-first
inventory includes requests awaiting reasoning and retained terminal metadata;
it does not dispatch, touch fairness timestamps or expose command bodies.
The registered Linux workcell can provision official Go and Rust toolchains into
its runtime root and recover the original process after a lost acknowledgement.
Workcell inventory distinguishes a missing executable from a failed version
measurement. Each present-tool probe has a five-second ceiling; the whole
confined inventory has a sixty-second ceiling. A timeout or failed probe cannot
attest an absent capability or a partial snapshot. Exact source and environment
digest checks still apply before execution.

Cancellation of an acknowledged pre-start source, capability or inventory failure
requires a separate authenticated absence observation from the healthy process
journal, bound to the original operation, request key and command contract. A
generic failed code never proves absence. Legacy recovery results without that
receipt can obtain one deterministic versioned recovery-only observation; this
cannot start or replay the command. Missing, unavailable or conflicting evidence
remains pending for reconciliation.
Install and verify the receipt-capable signed Edge before upgrading the control
plane that requests the versioned legacy observation. An older Edge cannot supply
that receipt; its terminal generic failure remains pending rather than generating
another observation key.
After a captured process stop fails or is cancelled, the coordinator observes
that exact process through the existing scoped status route. Only an authenticated,
identity-matching terminal result with a known exit settles cancellation. Pending,
failed, foreign or unknown-exit observations remain unresolved. A still-running
observation schedules another read, not another stop or command start; lost read
acknowledgements recover the captured observation from the existing journal.
The private request durably retains the existing `reconciliation_required` reason
while cancelling a captured process. Terminal stop-record retention cannot erase
that read-only phase. Ordinary pre-cancellation reads are cleared atomically when
cancellation is admitted. Older cancelling observations without a retained stop
binding remain unresolved instead of authorizing another stop. No table or schema
version changes; legacy cancellation-requested records remain readable. Rollback
to a backend that predates this state/reason combination requires settling these
requests or restoring its matching pre-rollout database snapshot.
An unwatched Unix worker's finite stop wait reconciles its private exit receipt
after daemon restart, matching Windows behavior. Active watchers retain ownership
of their wait result, and live or unavailable identities receive no extra signal.
The separately opted-in isolated runner supports fixed public-source commands;
its configuration, calibration and recovery contract are documented in
`docs/development-runner.md`.

Deterministic execution continues without an attached consumer chat. Code failures
and requirements that need reasoning remain explicitly `awaiting_reasoning`;
there is no background model inference or automatic rewrite of source.

Production rollout, real-device concurrency/restart checks and BuildKit's complete
`make validate-all` remain acceptance gates. Record their actual outcomes before
claiming deployment or workflow completion.

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
