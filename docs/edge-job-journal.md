# P16 durable Edge job journal

Status: **P16 Step 4 implemented on `p16-global-work-scheduler`; exact-head validation pending.**

This document is the source of truth for local development-workcell execution durability. It extends the earlier outbound Edge protocol without changing the public task API or permitting a second execution after an ambiguous crash.

## File, schema, and bounds

The journal is a private SQLite file owned by the Edge user:

```text
~/.local/state/mcp-edge/journal.db
```

The file must be a regular non-symlink file with mode `0600`. SQLite uses one connection, `synchronous=FULL`, a five-second busy timeout, DELETE journaling and a maximum page count of 4096. Schema version `2` adds:

```text
attempt
result_id
lease_id
delivered_at
updated_at
```

The logical entry limit is `4096`. A new execution fails closed when capacity is exhausted. Existing entries remain readable at capacity so pending results can still be delivered or reconciled.

Opening the journal migrates the legacy unversioned schema in place. Existing `started` and `completed` rows are retained. Legacy completed results receive the same deterministic result identity they would have received under schema version 2. A schema newer than the packaged reader fails closed.

## Durable identities

One execution is keyed by the control-plane idempotency key and exact task ID. Reusing an idempotency key for another task is rejected.

The first leased attempt is persisted and remains the local execution attempt even if the control plane later re-leases the task for completion delivery. A completion receives a deterministic identity:

```text
jr_<sha256>
```

The digest binds version, idempotency key, task ID, outcome, bounded summary and optional result reference. Replaying the same result therefore preserves one identity. A different result conflicts instead of replacing the durable completion.

## Required write ordering

The runner follows this order:

1. Persist `started` before calling the executor.
2. Execute at most once.
3. Persist `completed`, result body and `jr_<sha256>` before any completion request.
4. Send the completion to the control plane.
5. Persist `delivered_at` only after the control plane acknowledges it.

If step 4 fails, the completed result remains pending. When the task is leased again, the runner sends the stored result and does not call the executor again.

A process restart that finds only `started` does not guess whether an external side effect occurred. It stores and delivers the stable manual-reconciliation result:

```text
previous execution was interrupted; manual reconciliation required
```

There is no blind retry.

## Offline continuation

A transient heartbeat failure no longer immediately cancels a bounded local stage. The runner keeps executing locally and retries the heartbeat. Defaults are:

```text
heartbeat: derived from the lease, capped at 5s
reconnect interval: 5s
lease: 10m
offline grace: 10m
lease safety margin: up to 30s
```

The task's approved local maximum duration remains authoritative. Offline continuation is also bounded by the last lease expiration confirmed by the control plane. The effective offline deadline is the earlier of the ten-minute grace and the confirmed lease expiration minus the safety margin. A heartbeat success replaces that authority deadline with the newly returned expiration. The Edge therefore stops before the server may reassign the same task.

If connectivity returns before the effective deadline, heartbeats continue and the locally completed result is delivered normally. If the task completes while the endpoint remains unavailable, the result is persisted and delivered after reconnection.

When the offline grace expires, the runner cancels the execution context and stores:

```text
offline grace exceeded; manual reconciliation required
```

The workcell checks cancellation before every stage, so no new stage starts after remote cancellation, the local kill switch, deadline cancellation or offline-grace expiry is observed. There is no silent VPS fallback.

## Retention and cleanup

Pending completion delivery is attempted before requesting any new lease. A failed pending delivery blocks that runner cycle; it cannot be bypassed by leasing another task.

Completed but unacknowledged results are never silently deleted. They remain pending until delivery or operator reconciliation.

Acknowledged results are eligible for cleanup after seven days by default. Cleanup occurs before leasing new work and deletes only rows with `delivered_at`. Pending and `started` rows are retained. The entry limit and SQLite page limit bound storage growth; capacity exhaustion blocks new execution instead of discarding evidence.

## Doctor states

`mcp-edge doctor` inspects the journal read-only. It does not create, migrate, delete or repair the database. The bounded `journal=` value is one of:

```text
empty
ready
pending
reconciliation
migration_required
blocked
```

Meanings:

- `empty`: no journal file exists yet.
- `ready`: schema and integrity are valid, and every completion is acknowledged.
- `pending`: one or more completed results await acknowledgement.
- `reconciliation`: one or more `started` rows survived interruption.
- `migration_required`: a valid legacy schema exists and will migrate when the runner opens it.
- `blocked`: file layout, ownership, mode, SQLite integrity, schema or bounds are unsafe.

`pending`, `reconciliation` and `migration_required` make the installation degraded. `blocked` fails doctor with an explicit unsafe-journal error. Doctor output never exposes task IDs, idempotency keys, result IDs, paths, summaries or result bodies.

## Recovery and rollback

Normal recovery is idempotent:

- restart the Edge service;
- allow completed pending results to replay;
- manually review an interrupted `started` execution before scheduling replacement work;
- use the existing local `STOP` file to prevent new leases while reviewing.

Rollback to an older binary must not delete `journal.db`. An older reader that cannot understand schema version 2 must fail closed; restore the compatible signed release instead of editing SQLite manually.

## Verification

Step 4 tests cover:

- `started` before executor invocation;
- `completed` before delivery;
- lost completion replay without re-execution;
- crash-after-start manual reconciliation;
- transient disconnect and reconnect;
- offline-grace expiry;
- no new workcell stage after cancellation;
- stable attempt and result identity;
- legacy schema migration;
- delivered-only cleanup and capacity bounds;
- read-only doctor states and unsafe/corrupt journal rejection;
- close/reopen persistence.

Exact-head CI remains authoritative for race, Debian migration, Bubblewrap, Rootless Podman and distributed OpenCode gates.
