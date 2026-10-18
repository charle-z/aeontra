# Tasks — P16 Global Work Scheduler and Edge lifecycle

Status: **test-first backlog**. A task is complete only when its RED test, implementation,
regression/adversarial tests, docs, and rollback evidence are all present.

## Step 0 — Contract and documentation

- [x] Add a docs test that requires the P16 spec, threat model, plan, tasks, ADR, and
  dated capacity baseline.
- [x] Add assertions for the non-negotiable phrases/concepts: no silent target fallback,
  no opaque IDs in normal UX, maximum one initial VPS build, lossless workspace
  recovery, two-command clean Edge install, local Edge revalidation, and cgroup
  enforcement.
- [x] Add the P16 entries to `docs/documentation-map.md`.
- [x] Run docs tests and `git diff --check`.
- [x] Commit as `Step 0: Specify global scheduler and Edge lifecycle`.

## Step 1 — Migration inventory

### RED tests

- [x] Fixture: preferred P15 state with identity/workspaces survives repeat inventory.
- [x] Fixture: legacy `~/.config/mcp-devbox-edge` migrates only when preferred identity
  is absent.
- [x] Fixture: directory named `p12` with unknown contents is reported and untouched.
- [x] Fixture: known P12/P15 service, release, repository, and workspace-state layouts
  are classified read-only.
- [x] Reject terminal/ancestor symlinks, symlinked identity markers, conflicting or
  occupied state, unsafe state owner/mode, and invalid signed-release links.
- [x] Inject failure after every migration stage and prove rollback/idempotence.
- [x] Reject Windows-mounted, overlapping, dirty, mismatched, or ambiguous project
  workspaces in the Step 3 workspace resolver; Step 1 does not guess mount semantics.

### Implementation

- [x] Add typed inventory records and stable blocker codes.
- [x] Add schema version and private migration journal.
- [x] Add a closed safe inventory/migrate/recover CLI with no path or command input.
- [x] Add atomic no-replace migration, verification, crash recovery, and rollback.
- [x] Document supported legacy layouts, unknown-directory policy, recovery, and
  explicit Step 2 limitations.

## Step 2 — Package, onboard, doctor, repair

### RED tests

- [x] Clean package install contract followed by one guided onboard; remote container
  execution remains an exact-head gate.
- [x] Repeat package transaction is byte/invariant idempotent for identity, key,
  workspaces, and checkpoint fixtures.
- [x] Existing valid identity skips pairing and stdin consumption.
- [x] Rootless socket missing/unsafe blocks readiness without partial activation.
- [x] Failed service health rolls state back before the previous signed release is
  restored; remote container execution remains an exact-head gate.
- [x] `doctor --repair` repairs only fixed owned resources.
- [x] Repair cannot accept command, URL, arbitrary path, script, hash, or repository
  mutation.
- [ ] WSL/systemd restart returns Edge to healthy without commands.

### Implementation

- [x] Finish package postinst/preflight around the migration inventory.
- [x] Reuse the persisted human identity name as the normal onboarding alias and omit
  opaque device IDs from normal output.
- [x] Add closed doctor status codes and the fixed signed-installation repair profile.
- [x] Add package migration compatibility and rollback around existing signed update
  behavior.
- [x] Add install/update/uninstall docs; uninstall preserves repos/state by default.

## Step 3 — Project aliases and workspace resolver

### RED tests

- [x] Resolve one registered project using alias only, with no opaque ID or path in the
  safe result.
- [x] Infer the canonical checkout name from owner/repository without mutating the
  filesystem.
- [x] Reuse and revalidate one explicitly bound matching checkout.
- [x] Associate one unique safe legacy path without moving it through internal
  preview/revalidation/apply.
- [x] Clone missing repo into approved root with fixed Git authority.
- [x] Dirty, mismatched, ambiguous, symlinked, escaped, or Windows-mounted checkout
  blocks safely.
- [x] Multiple unbound matching checkouts produce an explicit ambiguous result.
- [x] Alias collision, case folding, Unicode confusable, traversal, and oversized input
  fail.
- [x] Restart/reopen preserves aliases and bindings.

### Implementation

- [x] Add private versioned `projects.db` schema and fail-closed schema validation.
- [x] Add owner-bound repository canonicalization.
- [x] Add bounded unbound local workspace discovery/classification.
- [x] Add lossless recovery decision engine and stable reasons.
- [x] Add read-only project discover/resolve/status tools first.
- [x] Add internal association preview/apply with revalidation and compensating rollback.
- [x] Expose approved create/associate tools without internal IDs.

## Step 4 — Edge durable job journal

### RED tests

- [x] Persist `started` before executor invocation.
- [x] Persist `completed` before result delivery.
- [x] Lost completion delivery replays one result without execution.
- [x] Crash after start enters reconciliation/manual-review, not blind rerun.
- [x] Network disconnect during bounded stage completes locally and reconnects.
- [x] Disconnect beyond grace blocks new stages.
- [x] Revocation/cancellation is observed and no new stage starts.
- [x] WSL/process restart preserves job/result state.

### Implementation

- [x] Add local journal schema/state machine.
- [x] Add offline grace/deadline/reconnect loop.
- [x] Add stable attempt and result identity.
- [x] Add pending-result retention/cleanup bounds.
- [x] Add local doctor checks for journal integrity.

## Step 5 — Scheduler store

### RED tests

- [x] Validate all legal and illegal job transitions.
- [x] Concurrent equal enqueue returns one job.
- [x] Concurrent different enqueue respects global/per-workspace bounds.
- [x] One active lease per job/pool and fencing token.
- [x] Expired lease recovery and stale completion rejection.
- [x] Dependency success/failure/block propagation.
- [x] Cancel queued/running behavior.
- [x] Crash/reopen consistency and integrity check.
- [x] Database/page/row/output bounds.
- [x] Unsupported multi-control-plane writer configuration fails closed.
- [x] Race detector and fuzz targets.

### Implementation

- [x] Add `internal/workqueue` store and migrations.
- [x] Add typed IDs, states, reasons, leases, attempts, dependencies, results.
- [x] Add dedup unique identity.
- [x] Add private configuration/path validation.
- [x] Add backup/restore fixture and operational docs.

## Step 6 — Admission and fairness

### RED tests

- [x] Resource-vector parser rejects negative, NaN, infinity, overflow, and unknown
  dimensions.
- [x] Hard admission never exceeds CPU/RAM/I/O/PIDs/slot budgets.
- [x] Deficit Round Robin prevents one workspace monopoly.
- [x] Aging prevents starvation within configured bounds.
- [x] Deterministic scheduling under equal timestamps/costs.
- [x] Queue backpressure and TTL cleanup.
- [x] EWMA update clamps outliers and requires minimum samples.
- [x] Estimates remain separate per device/pool/profile.
- [x] Shadow score never changes authorization/target/maxima.

### Implementation

- [x] Add administrator-owned resource profiles and pool registry.
- [x] Add DRR deficit accounting and bounded aging.
- [x] Add wait estimate and safe queue metrics.
- [x] Add EWMA history and shadow score.
- [x] Add disable/reset controls and docs.

## Step 7 — VPS builder spike

### RED/acceptance harness

- [x] Detect/reject rootful Docker sockets and symlinked rootless endpoints.
- [x] Verify selected builder runs as dedicated non-root identity.
- [x] Verify cgroup contains builder plus child compilers/helpers.
- [x] Verify cancel terminates the whole process group.
- [x] Verify cache is reusable and bounded.
- [x] Verify output and artifact identity are bounded/redacted.
- [ ] Measure 50/65/80 percent quotas with same commit.
- [ ] Record control-plane health latency and 502 count.

### Implementation/spike

- [x] Prototype rootless BuildKit.
- [ ] Prototype rootless Podman only if BuildKit misses a structural requirement.
- [ ] Select engine in a follow-up ADR amendment/baseline.
- [x] Add installation/service/remove scripts for the selected private builder.
- [x] Do not register public tools yet.

## Step 8 — VPS worker

### RED tests

- [ ] Worker leases only configured pool/profile.
- [ ] Effective resources never exceed local maxima.
- [ ] Twenty simultaneous requests yield exactly one active build.
- [ ] Worker restart reconciles running/external state safely.
- [ ] Control-plane restart does not lose build/result.
- [ ] Timeout/cancel cleans child processes/networks/temp resources.
- [ ] Public MCP container has no builder/rootful socket.
- [ ] Metrics exclude prompts, commands, secrets, paths, and source content.

### Implementation

- [ ] Add worker binary/service/container.
- [ ] Add authenticated private lease/result channel.
- [ ] Add profile adapter for selected rootless builder.
- [ ] Add cgroup/resource enforcement and metrics collector.
- [ ] Add operational env reference and Coolify deployment docs.

## Step 9 — Platform queue integration

### RED tests

- [ ] Existing platform preview plan binding remains exact.
- [ ] Existing tool schemas and annotations remain compatible.
- [ ] Deploy execution enqueues and returns durable state.
- [ ] Lost Coolify response reconciles before retry.
- [ ] Force/no-cache jobs remain distinct.
- [ ] Concurrent chats do not trigger concurrent platform builds.
- [ ] Scheduler-managed auto/manual deploy detection and stable reason.

### Implementation

- [ ] Add platform job payload/reconciliation adapter.
- [ ] Add job/deployment state mapping.
- [ ] Add compatibility switch and drain/rollback procedure.
- [ ] Add break-glass documentation.

## Step 10 — Per-Edge pools

### RED tests

- [ ] Device A and B may run independently.
- [ ] One device cannot exceed local CPU/RAM/PIDs/slots.
- [ ] Remote values above local max are denied/clamped.
- [ ] Offline device blocks only its own pool.
- [ ] No Edge-to-VPS fallback.
- [ ] Local capacity change requires administrator action/re-attestation.
- [ ] Parrot defaults to one heavy build until calibration.

### Implementation

- [ ] Add safe capability report/attestation.
- [ ] Add per-device pool creation/update/revocation.
- [ ] Add local resource-token admission.
- [ ] Add per-device calibration history.

## Step 11 — Images and registry

### RED tests

- [ ] Validate allowed registry/repository/tag syntax and bind digest.
- [ ] Separate Git, registry-push, and platform-pull credentials.
- [ ] No credential in argv/log/result/audit/Git.
- [ ] Build/push/deploy dependency DAG.
- [ ] Push failure blocks deploy.
- [ ] Tag mutation cannot alter approved digest.
- [ ] Roll back to previous digest.
- [ ] Edge image deploy performs no VPS compilation.

### Implementation

- [ ] Add closed image-build profile(s).
- [ ] Add registry credential broker and allowlist.
- [ ] Extend Coolify app/create/deploy adapters additively for Docker Image source.
- [ ] Add health verification and digest rollback.

## Step 12 — Alias-first public UX

### RED tests

- [ ] Fresh chat resolves/continues/builds a real project with alias + target only.
- [ ] User is never asked for device/workspace/runtime/job/idempotency identifiers.
- [ ] Offline/missing/dirty/ambiguous states return actionable explanations.
- [ ] Consequential preview shows safe project/repo/target/resource summary.
- [ ] Low-level tools remain compatible for diagnostics.

### Implementation

- [ ] Register project resolve/status/continue/build tools.
- [ ] Add server-side idempotency generation and internal runtime resolution.
- [ ] Add console project/job/pool views.
- [ ] Update prompt/help/catalog docs.

## Step 13 — Closure

- [ ] Clean P16 install on disposable Parrot/WSL fixture.
- [ ] Upgrade real/representative P12/P15 state without re-pairing.
- [ ] Disconnect/reconnect build proof.
- [ ] 20-request VPS concurrency proof.
- [ ] 50/65/80 quota production calibration and dated baseline.
- [ ] Real VPS build and real Edge image build/deploy health proof.
- [ ] All adversarial, race, static, vulnerability, SBOM, secret, image, rootless,
  PostgreSQL/Chromium, package, migration, and docs gates green at exact PR head.
- [ ] README/security/install/tools/env/backup/rollback/limitations docs updated.
- [ ] PR reviewed and merged without direct main/force push.
- [ ] Production exact commit/catalog/health verified.
- [ ] Signed Edge release and annotated `p16` tag published.
