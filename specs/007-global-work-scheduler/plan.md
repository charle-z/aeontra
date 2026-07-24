# Plan — P16 Global Work Scheduler and Edge lifecycle

Status: **Step 0 in progress**. Each step is independently reviewable, test-first,
rollback-capable, and must leave existing production behavior compatible until its
replacement path is proven.

## Delivery rules

- Work only on branch `p16-global-work-scheduler`; never commit directly to `main`.
- Commits use `Step N: <effect>` and contain one coherent reversible stage.
- Existing public tools retain names, schemas, annotations, and semantics unless an
  additive field/tool is explicitly reviewed.
- New behavior remains disabled or shadow-only until its step-specific tests and
  migration/rollback evidence pass.
- Every state/schema/config change has an explicit downgrade or safe-stop story.
- No production closure without exact-head remote gates, dated baseline, live smoke,
  rollback evidence, and an annotated `p16` tag.

## Step 0 — Contract, evidence, and documentation

Deliver:

- `spec.md`, `threat-model.md`, `plan.md`, and `tasks.md`;
- ADR 0004 selecting one durable scheduler with separated execution pools and a
  simplified Edge lifecycle;
- dated 2026-07-22 capacity baseline;
- documentation-map entry;
- RED documentation tests for mandatory invariants and configuration names.

No runtime behavior changes.

Rollback: remove the Step 0 commit/branch; production is untouched.

Exit gate:

- clean tree before/after;
- docs tests show the new contract is discoverable and internally consistent;
- threat-model stop conditions reviewed;
- branch starts from current `origin/main`.

## Step 1 — P12/P15 inventory and migration model

Build a read-only classifier first for:

- preferred and legacy Edge state roots;
- signed releases/current link;
- known P12/P15 service units and compatibility paths;
- approved development/lab roots;
- existing workspace registrations and project candidates;
- unknown directories such as a user directory named `p12`.

Then add a transactional migration journal and versioned schemas for project/workspace
metadata. Unknown directories remain untouched.

Tests first:

- valid P12/P15 fixtures;
- mixed/partial fixtures;
- symlink and ownership attacks;
- unknown `p12` directory classified but not moved;
- identity/workspace bytes unchanged across repeat migration;
- rollback after every failure injection point.

Rollout: package migration runs in dry-run/report mode first.

Rollback: preserve old state, migration journal, and previous signed release; switch
back without rewriting opaque IDs.

Exit gate: migration is idempotent and proves no re-pairing/data loss for valid state.

## Step 2 — One-command package lifecycle and guided onboarding

Finish the P15 package candidate as the supported path:

```text
sudo apt install ./mcp-devbox-edge_<version>_amd64.deb
mcp-edge onboard --server <server>
```

Add/complete:

- automatic creation/validation of private roots;
- rootless socket enable/validation;
- alias assignment;
- packaged service activation and health smoke;
- existing-identity reuse;
- closed `doctor` and `doctor --repair`;
- atomic update/rollback around migrations and health.

Tests:

- clean install;
- repeat install;
- upgrade from P12/P15 fixtures;
- WSL restart;
- broken unit/link/socket/permission repair;
- failed new release health rolls back;
- repositories/workspaces are never deleted.

Rollback: previous signed bundle + previous service unit + compatible state snapshot.

Exit gate: at most two initial commands, zero manual commands for a normal signed
update, no pairing repeat when identity is valid.

## Step 3 — Durable project aliases and automatic workspace resolution

Add the project registry and high-level resolver:

- alias -> owner-bound repository identity;
- preferred target alias;
- one or more validated workspace bindings;
- profile/contract references;
- safe discovery, legacy association, and clone under approved roots.

Normal tools begin with read-only resolution/status. Mutation remains preview/approval
bound.

Tests:

- new chat resolves project by alias with no IDs;
- canonical path inference;
- existing safe checkout reuse;
- dirty/mismatched/ambiguous checkout blocks;
- symlink/Windows-mount/root escape rejection;
- repo rename/remote drift handling;
- alias collision and Unicode/confusable rejection;
- restart and migration persistence.

Rollback: aliases are additive metadata; existing low-level workspace tools remain.

Exit gate: one real project resolves and reports status by alias after restart.

## Step 4 — Edge continuity and result recovery

Decouple durable job/stage ownership from ChatGPT/OpenCode runtime lifetime.

Implement locally:

- job/attempt journal;
- started/completed/result-pending-upload transitions;
- reconnect with stable identity;
- offline grace and local deadline;
- cancellation/revocation observation;
- completed-result replay without re-execution;
- ambiguous started-state reconciliation.

Tests include process kill, WSL stop/start, network loss before/during/after build,
control-plane redeploy, stale lease, duplicate completion, and revocation.

Rollback: feature flag keeps current runtime path available; no schema downgrade until
new journal migration is proven.

Exit gate: a bounded fixture build survives disconnect and returns exactly one result
under the same job identity.

## Step 5 — Durable scheduler store and state machine

Implement `internal/workqueue` with a private SQLite database under `/state`:

- migrations;
- jobs/dependencies/pools/leases/attempts/results;
- unique deduplication identity;
- fencing;
- cancellation;
- expiration;
- integrity/bounds;
- backup/restore fixture.

No public build execution yet.

Tests:

- transition table and illegal transitions;
- concurrent enqueue/dedup/lease/complete;
- crash/reopen recovery;
- stale lease/result rejection;
- bounded growth/queue limits;
- corruption and unsupported multi-writer safe failure;
- race detector and fuzzed inputs.

Rollback: database is additive and unused by production tools until later steps.

Exit gate: deterministic concurrency/race suite green.

## Step 6 — Admission, fairness, estimates, and shadow scoring

Add:

- resource profiles and pool capacities;
- hard admission;
- Deficit Round Robin by project/workspace;
- bounded aging/backpressure;
- estimated wait;
- EWMA estimates per pool/device/profile;
- shadow priority score and metric output;
- finite/clamped arithmetic.

Enforce hard bounds/fairness; keep score-based reordering shadow-only.

Tests:

- no over-admission in any dimension;
- heavy and light starvation prevention;
- one project cannot monopolize;
- deterministic ordering;
- NaN/infinity/negative/overflow rejection;
- estimator poisoning/outlier clamps;
- no cross-Edge estimate sharing.

Rollback: disable scheduler ordering and retain FIFO/DRR hard-policy path.

Exit gate: simulation of thousands of jobs satisfies capacity/fairness invariants.

## Step 7 — Rootless VPS builder spike and selection

Before public tools, prototype rootless BuildKit under a dedicated worker/builder
boundary. Test rootless Podman only if BuildKit fails a structural requirement.

Matrix:

```text
CPU quota: 50%, 65%, 80% of one vCPU
same commit and no-cache/cached builds
```

Measure duration, CPU usage/throttling/PSI, memory peak/events, I/O pressure, health
latency, and 502 count.

Required capabilities:

- no rootful socket;
- whole process-group cgroup enforcement;
- cancellation;
- cache reuse;
- output/result bounds;
- safe cleanup;
- control plane remains healthy.

Rollback: spike is private/disabled; remove service and caches without touching repos
or production app.

Exit gate: choose and document the engine and production quota from evidence. If
neither engine meets requirements, stop P16 execution work rather than weakening the
boundary.

## Step 8 — VPS worker and queue integration

Add a separate worker service that:

- leases only allowed `vps.build`/`vps.deploy` jobs;
- revalidates profile/target/resources;
- invokes the selected rootless builder;
- renews lease and observes cancellation;
- persists metrics/artifact identity/result;
- reconciles after worker/control-plane restart.

Configuration is environment-driven with strict local maximums; cgroups enforce it.

Tests:

- 20 concurrent build requests, one active;
- worker crash/restart;
- control-plane restart;
- cancellation and timeout;
- lost result/reconciliation;
- requested resources above local max are clamped/denied;
- no Docker socket in public MCP container.

Rollback: existing direct platform flow remains behind a compatibility switch until
queue path passes live smoke.

Exit gate: real VPS fixture builds with zero observed 502 and exact concurrency.

## Step 9 — Existing platform tools through the scheduler

Route `platform_deploy*` through durable jobs while preserving existing inputs and
approval/revalidation semantics. Add only safe job/queue/deployment state output.

Add reconciliation for Coolify requests whose response is lost. Configure/detect
scheduler-managed applications and document break-glass.

Tests:

- exact plan binding still enforced;
- lost HTTP response does not blindly duplicate deploy;
- concurrent chats enqueue rather than invoke concurrently;
- manual/auto deploy detection;
- force/no-cache semantics preserved;
- existing catalog/annotation/contract tests remain green.

Rollback: compatibility switch returns to direct deploy after draining/marking queued
jobs; no job is silently lost.

Exit gate: one real scheduler-managed Coolify deploy completes and health verifies.

## Step 10 — Per-Edge pools and capability reporting

Extend Edge identity/status with bounded administrator-owned capacity and supported
profiles. Each device has independent build/runtime pools and local maxima.

Parrot starts at one heavy build until measured. Other Edges may advertise different
capacities only after local preflight/calibration.

Tests:

- two Edges can execute independently;
- one Edge cannot exceed its local resources/slots;
- one Edge offline does not block unrelated pool work;
- remote control plane cannot alter local maximum;
- capacity changes require local administrator action and re-attestation;
- no silent target migration.

Rollback: leave Edge build profiles disabled; existing runtime profile remains.

Exit gate: two simulated devices with different capacities schedule correctly.

## Step 11 — Image build, registry push, and image deploy

Add closed build-image profiles for VPS and Edge, immutable digest results, separate
registry credentials, and Coolify deploy-from-image support.

Flow:

```text
build@edge|vps -> optional push@same executor -> digest -> deploy@vps -> health
```

Tests:

- registry allowlist and credential separation;
- tag/digest race rejection;
- no secret in output/audit/Git;
- dependency failure blocks deploy;
- rollback to previous digest;
- Edge build causes no VPS compile;
- no GitHub Actions requirement.

Rollback: retain Dockerfile deployment mode; image mode is additive and opt-in.

Exit gate: real Edge -> registry -> Coolify image deploy -> health, with no VPS build.

## Step 12 — High-level project tools and UX closure

Add alias-first tools such as `project_resolve`, `project_status`,
`project_continue`, and project build preview/execute. Low-level IDs remain diagnostic.

Tests:

- clean new-chat prompts use project + target alias only;
- internal IDs are resolved and not requested from the user;
- missing/offline/recovery responses are actionable;
- no authority broadening through alias ambiguity;
- console displays project/job/pool state without secrets/paths.

Rollback: remove high-level registrations while retaining underlying compatible tools.

Exit gate: real project continuation/build from a fresh chat requires no copied ID.

## Step 13 — Enforcement closure, production evidence, and release

- disable/detect scheduler bypasses for managed applications;
- run adversarial and chaos suites;
- run measured 50/65/80 calibration and freeze defaults;
- prove clean install, migration, update, rollback, repair, disconnect/reconnect;
- update README, install docs, tools docs, environment reference, operations/backup,
  architecture, security, and limitations;
- create dated production baseline;
- open PR, require all exact-head gates green;
- merge without force-push/rebase/squash unless repository policy explicitly requires;
- verify production commit/catalog/health;
- publish signed Edge release and annotated `p16` tag.

Rollback: documented platform, worker, scheduler, database, Edge release, and image
rollback procedures are executed in a staging/fixture environment before closure.

## Definition of done

P16 is done only when installation simplicity, durable scheduling, zero-ID normal UX,
resource enforcement, Edge continuity, compatibility, and production evidence all pass.
Completing only the queue or only the installer is not P16 closure.
