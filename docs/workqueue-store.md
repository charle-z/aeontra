# P16 durable scheduler store

Status: **P16 durable task groups and fenced Edge workers implemented in source; exact-head and real-device acceptance pending.**

`internal/workqueue` is the private coordination store for admission, VPS workers and
per-Edge pools. It still grants no execution authority by itself. The public
`project_task_*` tools connect its identities to the existing signed Edge operation,
workspace and model-turn authorities; the queue never receives a host path, credential,
command, source file or model response.

## Storage and writer model

The control plane opens one private root and stores:

```text
/state/workqueue/queue.db
/state/workqueue/queue.lock
```

The root is a real non-symlink directory with private permissions. `queue.db` is SQLite
schema version 2, mode `0600`, WAL, `synchronous=FULL`, foreign keys, bounded pages and
one database connection. A non-blocking advisory lock allows exactly one active
control-plane writer and releases automatically when the process exits. `Writers`
values other than one fail closed. Redis and additional resident queue services are not
required. Version-1 databases migrate transactionally to schema version 2; future
schema versions fail closed. The optional acceptance contract, receipt and cleanup
checkpoint are additive columns migrated in one transaction without changing
`PRAGMA user_version=2`, so a previous v2 binary can open the database and ignore them.
That older binary does not enforce the acceptance contract: stop task coordination and
take a private backup before rollback, and do not let an older binary operate on
contract-bearing tasks. If recovery must continue under the older binary, restore the
pre-upgrade backup using the documented restore procedure.

The persisted controller identity must match on reopen. A second controller identity, future schema, unsafe symlink/layout, corrupt database, row overflow or storage above 64 MiB blocks opening.

## Jobs and transitions

A job stores only coordination metadata:

```text
job ID
idempotency key
workspace alias
immutable pool
resource profile
payload SHA-256
state and stable reason
attempt and fencing counter
lease identity/holder/expiration
bounded terminal summary and optional result reference
```

The queue contains no source content, prompts, commands, credentials or filesystem paths.

Legal states are `blocked`, `queued`, `leased`, `succeeded`, `failed` and `cancelled`. Every other transition fails closed. Terminal jobs do not return to a runnable state.

## Deduplication and bounds

The idempotency key is globally unique. Concurrent identical enqueue calls return the same job; changing workspace, pool, profile, payload hash or dependency set conflicts. Dependency order does not change identity.

Defaults are 1024 pending jobs globally and 64 pending jobs per workspace, configurable only downward/upward within reviewed hard caps. A transaction enforces both bounds under concurrent enqueue; terminal evidence does not consume pending capacity. Lists are capped at 100 and dependencies at 16.

## Leases and fencing

One queued job may receive one lease for its immutable pool. A lease increments both attempt and fencing counter. Heartbeat and completion require exact job ID, lease ID and fence. Expired leases return to the back of the pending queue unless cancellation was requested. After four abandoned leases, the job terminates as `failed` with `recovery_exhausted` instead of cycling forever. A completion from an older fence is rejected even if it carries an otherwise valid result.

Running cancellation sets `cancel_requested`; heartbeat exposes it and only a cancelled terminal result is accepted afterwards. Queued or blocked cancellation becomes terminal immediately.

## Durable task groups and managed workers

One task group records an idempotency key, project and target aliases, exact base commit,
combined goal digest, worker count, execution timeout and timestamps. It owns one to four
worker jobs. Each worker stores only a private staged-goal reference plus opaque operation,
worktree, workspace and runtime identities. The goal body stays in the bounded model-turn
store and never appears in task status.

The model-turn store keeps active task goals with quota-counted private ownership pins
keyed by the task idempotency digest. Startup reads the complete bounded set of active
worker goal references and restores those pins before the first expiry cleanup; the
ordinary one-hour runtime-body TTL is unchanged for non-task goals. A queued worker whose
legacy goal body is already missing or has a different digest is failed with the bounded
`task_goal_unavailable` reason rather than starting from altered input. A worker already
leased or bound to a runtime is not cancelled; reconciliation stops before further Edge
effects and status reports `reconciliation_required` until the reference can be validated.
Terminal worker pins are released by reconciliation. Unpinned runtime goals retain their
ordinary expiry behavior. Cross-store pin-before-queue crashes leave a
bounded orphan pin that startup reconciliation removes; periodic reconciliation preserves
new pins for a five-minute staging/commit grace period. Pins remain inside the existing
model-turn quota and add no database or service.

The coordinator leases every worker independently. A new lease increments the fence;
the matching Edge worktree accepts a claim only for the same job and a strictly newer
fence. Startup records the durable Edge operation before waiting for it, binds the
returned managed worktree/workspace before creating the runtime, and derives its private
lifecycle state from the bound runtime. A periodic coordinator reconciles these identities
after control-plane restart without creating duplicate worktrees or runtimes. Its bounded
scan includes only nonterminal groups, so retained historical evidence cannot starve newer
work. If the process exits between creating an Edge operation and storing its opaque ID,
the coordinator recovers that exact operation by its server-owned idempotency key instead
of dispatching a duplicate.

`project_task_start` provides bounded fan-out, not implicit source integration. A model
runtime reaching `completed` proves only that the model loop ended; it does not prove
that the requested goal was satisfied. `project_task_status` therefore reports private
queue `lifecycle_state`, live `runtime_state`, and a separate `acceptance_state`.
For a completed runtime it revalidates the exact managed worktree and returns only bounded
Git evidence: base and head commits, cleanliness, commits ahead of base and changed-path
count. Valid evidence yields `acceptance_pending`; unavailable, stale or inconsistent
evidence yields `reconciliation_required`. P16 deliberately has no generic automatic
`accepted` transition because acceptance criteria depend on the task.

`project_task_start` may optionally store a typed version-1 Git evidence contract in the
same workqueue row. It requires each worker's live worktree to be clean and to meet the
contract's minimum commits-ahead and changed-path counts relative to the exact task base.
When every worker's live evidence matches the exact task, worker, worktree, workspace,
branch, base, lease and fence and satisfies the configured predicate, status records a
durable Git evidence receipt. This sets `git_evidence_state` to `verified`; semantic
`acceptance_state` and the task/worker state remain pending until a trusted objective and
test evaluator exists. The receipt binds those identities and counts to a digest of every
contract field and records when the Git evidence was checked. Before removing a succeeded
worker's worktree under this contract, cleanup revalidates the same receipt against live
Edge evidence; a missing or changed worktree remains `reconciliation_required`. After
successful cleanup, the Git receipt and cleanup marker preserve the verified Git
predicate, not semantic acceptance. Cleanup uses a stable server-derived key per task and
worker, so retry after an Edge success/SQLite-marker crash recovers the same operation
even if the caller supplies a new retry token. The marker is written only
after the exact cleanup operation succeeds. The predicate is not proof that a
natural-language goal was satisfied or that tests passed. The Edge status contract does
not expose changed paths, test results, or an identity for dirty/untracked contents, so
v1 cannot make those claims. Contractless tasks—including existing tasks migrated from
schema version 2—remain `acceptance_pending` for explicit review; no global clean-worktree
or commit requirement is added, and dirty or uncommitted work remains inspectable and is
never removed by the evidence evaluator.

If a chat ends before retaining its task ID, `project_task_list` reads the local journal's
project/target index and returns at most 20 recent IDs, including terminal groups. It does
not poll an Edge, restart a runtime or expose goals. The caller then uses
`project_task_status` to reconcile the selected task before taking another action.

Each runtime-completed writer retains one explicit `codex/worktree-<id>` branch. Callers review and
combine those commits through normal Git and PR gates; the system never guesses conflict
resolution. `project_task_cleanup` requires a terminal task and exact current lease/fence.
For any succeeded worker covered by a Git evidence contract it additionally requires an
unchanged receipt-backed clean tree. The
caller key is a retry-correlation token; the Edge cleanup operation key is server-derived
from the task and worker. Cleanup removes the registered worktree but deliberately
preserves its Git branch, Git evidence receipt and durable task record. The task remains
semantically pending for review.

## Dependencies

A job with unfinished dependencies remains `blocked`. Successful completion of every dependency promotes it to `queued`. A failed or cancelled dependency propagates `dependency_failed` transitively and stores a bounded safe summary. Missing or duplicate dependency IDs fail enqueue.

## Backup and restore

`Backup` performs a full WAL checkpoint and writes one private bounded SQLite snapshot into an empty approved backup root. It never overwrites an existing snapshot. The snapshot is validated read-only for mode, size, SQLite integrity and exact schema.

`RestoreBackup` accepts only that validated snapshot, refuses an occupied destination, copies through a private temporary file and reopens the normal store so controller identity, bounds and integrity are revalidated. Backup files contain queue metadata and therefore remain private; they do not contain source or credentials by design.

## Verification

Tests cover legal/illegal transitions, equal and different concurrent enqueue,
global/per-workspace bounds, idempotency conflict and dependency-order normalization,
one active fenced lease, expiry recovery, stale completion rejection, dependency
success/failure propagation, queued/running cancellation, restart reconciliation,
task-group reuse/conflict, independent worker binding, schema migration, reopen/integrity,
automatic advisory-lock release after a real process exit, unsupported multi-writer
configuration, backup/restore, unsafe layout, unknown schema-zero databases and future
schemas, list/output bounds, race execution and fuzz input validation. Existing database
and lock ownership is validated; terminal summaries that resemble secrets fail closed.
The package is enforced by the atomic coverage gate at a 70% minimum.

This store alone grants no authority to execute work. All effects still pass through the
signed Edge, workspace, workcell and model-turn contracts.
