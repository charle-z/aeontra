# P16 durable scheduler store

Status: **P16 Step 5 implemented on `p16-global-work-scheduler`; exact-head validation pending.**

`internal/workqueue` is the private coordination store for later admission, VPS workers and per-Edge pools. It does not execute builds and exposes no public MCP tools in Step 5.

## Storage and writer model

The control plane opens one private root and stores:

```text
/state/workqueue/queue.db
/state/workqueue/queue.lock
```

The root is a real non-symlink directory with private permissions. `queue.db` is SQLite schema version 1, mode `0600`, WAL, `synchronous=FULL`, foreign keys, bounded pages and one database connection. A non-blocking advisory lock allows exactly one active control-plane writer and releases automatically when the process exits. `Writers` values other than one fail closed. Redis and additional resident queue services are not required.

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

## Dependencies

A job with unfinished dependencies remains `blocked`. Successful completion of every dependency promotes it to `queued`. A failed or cancelled dependency propagates `dependency_failed` transitively and stores a bounded safe summary. Missing or duplicate dependency IDs fail enqueue.

## Backup and restore

`Backup` performs a full WAL checkpoint and writes one private bounded SQLite snapshot into an empty approved backup root. It never overwrites an existing snapshot. The snapshot is validated read-only for mode, size, SQLite integrity and exact schema.

`RestoreBackup` accepts only that validated snapshot, refuses an occupied destination, copies through a private temporary file and reopens the normal store so controller identity, bounds and integrity are revalidated. Backup files contain queue metadata and therefore remain private; they do not contain source or credentials by design.

## Verification

Tests cover legal/illegal transitions, equal and different concurrent enqueue, global/per-workspace bounds, idempotency conflict and dependency-order normalization, one active fenced lease, expiry recovery, stale completion rejection, dependency success/failure propagation, queued/running cancellation, reopen/integrity, automatic advisory-lock release after a real process exit, unsupported multi-writer configuration, backup/restore, unsafe layout, unknown schema-zero databases and future schemas, list/output bounds, race execution and fuzz input validation. Existing database and lock ownership is validated; terminal summaries that resemble secrets fail closed. The package is enforced by the atomic coverage gate at a 70% minimum.

Step 6 will add resource vectors and fairness. Step 7+ will add executors. This store alone grants no authority to execute work.
