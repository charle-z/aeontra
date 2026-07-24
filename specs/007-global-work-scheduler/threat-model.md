# Threat model — P16 Global Work Scheduler and Edge lifecycle

Status: **Step 0 contract candidate**. Controls are requirements until implementation,
adversarial tests, exact-head CI, and production evidence prove them.

## Assets

- MCP Devbox control-plane availability on the 2-vCPU VPS.
- Edge device identities, aliases, private keys, project/workspace bindings, local
  contracts, checkpoints, jobs, and pending results.
- Source repositories and uncommitted local work.
- Scheduler state, leases, attempts, dependencies, deduplication identities, resource
  estimates, and audit records.
- Rootless builder sockets, caches, images, and immutable digests.
- Coolify authority and registry push/pull credentials.
- VPS/Edge CPU, memory, process, I/O, network, and disk capacity.
- Existing public tool semantics and approval boundaries.

## Trust boundaries

1. Chat/model clients may request intent but do not own infrastructure authority,
   resource maxima, device identity, paths, credentials, or approvals.
2. MCP Devbox is the normal admission/policy authority but is not trusted to enlarge an
   executor's local maximum.
3. Each Edge has an independent device identity, approved roots, profiles, capacities,
   credentials, revocation state, and local enforcement.
4. The VPS worker and rootless builder are separate from the public control-plane
   process.
5. Coolify remains an external deployment system. Direct/manual operations are
   break-glass and must be detected/audited where possible.
6. Repository files and model output are untrusted data. They cannot define commands,
   profiles, resource limits, priorities, paths, network policy, or credentials.
7. SQLite is durable scheduler truth for the single control plane; derived metrics and
   adaptive estimates are untrusted optimization inputs.
8. Signed Edge packages/releases are trusted only after signature, manifest, schema,
   preflight, and health verification.
9. Human-readable aliases are convenience identifiers, not authority grants.

## Threats and controls

| Threat | Required control |
|---|---|
| One or many chats saturate the VPS with builds | Global `vps.build` pool, hard max-active=1, cgroup/resource enforcement, queue bounds, fair scheduling, and backpressure. |
| Two entry points bypass the queue | All normal MCP deployment/build paths enqueue; scheduler-managed Coolify auto-deploy/webhooks disabled or detected; root/manual action documented as audited break-glass. |
| Limiting only Go still leaves Node/Docker saturating both CPUs | Limit the whole rootless builder/process group, not individual compilers or Dockerfiles. |
| Control plane and builder share one failure domain | Separate process/service/container; no build inside the public MCP process; independent restart and health checks. |
| Worker obtains rootful Docker authority | Reject `/var/run/docker.sock`, `/run/docker.sock`, root-owned or symlinked endpoints; accept only validated user-owned rootless BuildKit/Podman/Docker endpoints. |
| MCP control plane requests more resources than Edge policy permits | Edge revalidates locally; effective value is the minimum of request and local maximum; no remote mutation of local maxima. |
| Repository/model supplies huge, negative, NaN, infinite, or overflow estimates | Closed profiles, typed bounded parsers, finite arithmetic, clamping, fuzz/adversarial tests; repositories cannot set scheduler weights. |
| Priority formula grants authority | Authorization, target, approval, network, and resource maxima are evaluated before scoring; score can only order eligible work. |
| One workspace monopolizes the queue | Global/per-workspace bounds and Deficit Round Robin using bounded cost; starvation/aging tests. |
| Small jobs permanently starve heavy jobs or vice versa | Bounded aging, deficit accumulation, maximum wait assertions, shadow-mode validation before aggressive ordering. |
| Duplicate chat requests execute the same build many times | Atomic deduplication key/unique constraint; subscribers share one job/result; exact target/configuration/commit included. |
| Mutable image tag changes between approval and deploy | Bind artifact/deploy to immutable digest; tag may be metadata only. |
| Job silently changes Edge/VPS target | Immutable target in reviewed plan/job; new target requires new plan/job/approval; no fallback code path. |
| Edge disconnect causes duplicate build | Local durable journal before execution; stable job/attempt identity; replay completed result; reconcile `started` state rather than automatic rerun. |
| Edge disconnect causes work to run forever | Approved maximum duration, local deadline, offline grace, heartbeat/cancellation, blocked/manual-review state after grace. |
| Chat/browser disconnect kills work | Worker is a durable service; job ownership is independent of MCP request lifetime and model runtime. |
| Revoked Edge continues receiving work | Revocation blocks new leases immediately; active stage observes cancellation/expiry; local policy rejects new work without valid device state. |
| Compromised VPS expands Edge path/network/credential scope | Edge resolves opaque project/workspace locally and revalidates approved roots, repository identity, profile, network policy, registry scope, and credential class. |
| Alias resolves to wrong repository/path | Owner-bound canonical repository identity, local remote verification, path ownership/mode/symlink checks, collision/ambiguity blocking. |
| Automatic recovery overwrites uncommitted work | Discovery and clone are lossless-only; dirty/ambiguous/mismatched workspace blocks with one precise recovery reason. |
| Legacy `p12` directory is deleted or renamed incorrectly | Classify known legacy roots/services/state first; unknown directories are never automatically moved/deleted; migration journal and rollback. |
| Unknown `p12` directory is mistaken for product state | Treat an unknown `p12` directory as untrusted user data, classify it read-only, and leave it untouched unless a reviewed migration rule matches. |
| Package update loses identity/workspaces/jobs | State outside release tree; transactional versioned migrations; pre/post byte/invariant tests; atomic release link; rollback on health failure. |
| Partial update leaves unusable Edge | Staging verification, signed manifest, schema compatibility check, activation smoke, previous release rollback, closed repair operation. |
| Repair becomes a host shell | `doctor --repair` exposes fixed repair profiles only; no arbitrary command/path/URL/hash input; tests prove repositories are untouched. |
| Pairing repeats after normal update/redeploy | Existing valid identity is reused; pairing only when absent/revoked/invalid; migration tests and acceptance smoke. |
| Workspace/runtime IDs remain required UX | High-level alias tools resolve internal IDs; low-level tools remain diagnostic; tests run new-chat flows without ID input. |
| Scheduler database corrupts or grows without bound | Private jailed path, migrations, WAL/transactions, page/row/queue bounds, integrity check, backup/restore test, safe startup failure. |
| Lease expiry causes concurrent attempts | Transactional lease acquisition, unique active attempt constraint, fencing token/lease ID, stale completion rejection. |
| Worker crashes after external side effect | Stage-specific idempotency, persisted external identity (image digest/deployment ID), reconciliation before retry, no blind replay. |
| Coolify call succeeds but response is lost | Persist requested operation and reconcile Coolify deployment state before retrying; duplicate deploy prevention where platform identity allows. |
| Build output/log leaks secrets | Existing redaction, bounded results, no environment dump, no credential in argv/output/audit, registry auth kept in executor store. |
| Git credential reused as registry authority | Separate Git, registry-push, and Coolify registry-pull credentials and stores. |
| Queue metrics expose private paths/projects/targets | Safe aliases/opaque IDs and aggregate metrics only; no raw path, prompt, command, private target, secret, or source content. |
| Adaptive estimator is poisoned by one anomalous run | Minimum sample count, bounded EWMA change, profile clamps, per-target history, outlier handling, shadow mode, reset/disable switch. |
| Metrics from one Edge tune another | Per-pool/per-device profiles; no cross-device adaptation without explicit administrator decision. |
| SQLite becomes a bottleneck under future replicas | First release explicitly single-control-plane; storage interface isolated; fail closed against unsupported multi-writer deployment. |
| Redis absence loses performance | Queue volume is low/moderate and correctness-bound; benchmark and DB bounds must prove SQLite is sufficient. |
| Direct root access cannot be prevented | Root/manual operations are outside normal zero-trust enforcement; document, minimize, and audit break-glass rather than claiming impossibility. |

## Zero-trust invariants

- Authenticate and authorize every job, target, lease, attempt, result, and privileged
  effect; network location or prior pairing alone is insufficient.
- Use least privilege and separate credentials for Git, registry push, and platform
  pull/deploy.
- Treat control-plane decisions as proposals that local executors independently
  revalidate.
- Make authority short-lived, exact, single-purpose, revocable, and auditable.
- Deny by default when identity, path, target, policy, capacity, state, or reconciliation
  is ambiguous.
- Optimization equations never grant or expand authority.

## Stop conditions

P16 must not merge or deploy when any test/review shows:

- more than one active heavy VPS build under the initial policy;
- a path that executes a build outside the selected rootless/cgroup boundary;
- any accepted rootful Docker socket;
- silent target fallback or target mutation;
- duplicate execution after disconnect, lease expiry, retry, or lost completion;
- identity/workspace/job loss during package update or server redeploy;
- a normal project flow requiring the user to supply opaque IDs;
- automatic move/delete of an unknown legacy directory;
- automatic clone/recovery that can overwrite dirty local work;
- repair accepting free-form commands, paths, URLs, or scripts;
- unbounded queue, database, output, log, metric, or adaptive value;
- repository/model control over profile, weight, maximum, network, credential, or
  priority class;
- raw secrets, paths, commands, prompts, or private targets in tool/audit/metrics output;
- stale lease/result accepted after fencing changed;
- a scheduler-managed Coolify path that can create concurrent builds without detection;
- 502/control-plane outage during the required measured calibration;
- regression to existing public tool contracts or P15 signed-update guarantees.

## Residual risks

- A root administrator can always bypass normal policy. P16 limits and audits the normal
  path; it cannot make the host owner powerless.
- Rootless container engines still provide substantial authority to the owning Edge or
  worker user. Separate users, roots, profiles, network policy, and cgroups reduce but
  do not eliminate impact from an engine compromise.
- A completed offline stage may finish after central revocation until its local
  credential/lease/deadline check stops the next stage. Offline grace is deliberately
  bounded and must be measured against usability.
- Resource estimates will be imperfect. Hard cgroup limits protect the host while the
  estimator learns.
- Coolify may expose platform behaviors that are not perfectly idempotent. P16 must
  reconcile external state and document any remaining duplicate-risk window.
- A human-readable alias can be socially confusing even when technically unambiguous;
  status output must show safe repository/target summaries before consequential plans.
