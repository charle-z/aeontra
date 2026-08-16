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
required. Version-1 databases migrate transactionally to the task-group schema; future
schema versions fail closed.

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

Defaults are 1024 jobs globally and 64 per workspace, configurable only downward/upward within reviewed hard caps. A transaction enforces both bounds under concurrent enqueue. Lists are capped at 100 and dependencies at 16.

## Leases and fencing

One queued job may receive one lease for its immutable pool. A lease increments both attempt and fencing counter. Heartbeat and completion require exact job ID, lease ID and fence. Expired leases return to queue unless cancellation was requested. A completion from an older fence is rejected even if it carries an otherwise valid result.

Running cancellation sets `cancel_requested`; heartbeat exposes it and only a cancelled terminal result is accepted afterwards. Queued or blocked cancellation becomes terminal immediately.

## Durable task groups and managed workers

One task group records an idempotency key, project and target aliases, exact base commit,
combined goal digest, worker count, execution timeout and timestamps. It owns one to four
worker jobs. Each worker stores only a private staged-goal reference plus opaque operation,
worktree, workspace and runtime identities. The goal body stays in the bounded model-turn
store and never appears in task status.

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

Each runtime-completed writer retains one explicit `codex/worktree-<id>` branch. Callers review and
combine those commits through normal Git and PR gates; the system never guesses conflict
resolution. `project_task_cleanup` requires a terminal task, exact current lease/fence and
a clean worktree. It removes the registered worktree but deliberately preserves its Git
branch and the durable task record.

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
