# Direct Edge operation lifecycle

Direct GPT Web actions use the existing server-owned `edge_operations` journal. This layer is transport and lifecycle infrastructure; it does not yet accept arbitrary commands or scripts.

## Durable states

An operation moves through a closed state machine:

- `queued`: persisted and waiting for its paired Edge.
- `leased`: assigned to one Edge process under a signed, expiring lease.
- `succeeded`: completed with a validated bounded result.
- `failed`: completed with a stable safe code and no result body.
- `cancelled`: cancellation won before a terminal success or failure.

Terminal states are immutable. An expired normal lease returns to `queued`. An expired lease with cancellation requested becomes `cancelled` instead of being executed again. Status reads, active-operation listings, cancellation requests and idempotent retries normalize expired leases immediately; they do not wait for the Edge to poll again. Requeued attempts clear prior progress so the new lease can restart progress at revision `1`.

## Progress and restart recovery

While executing an operation, the Edge sends signed progress heartbeats. Progress contains only:

- a strictly increasing revision;
- a closed safe phase name;
- optional completed and total unit counters.

A valid progress heartbeat renews the lease for one bounded heartbeat window. Progress JSON is capped at 1 KiB, counters are bounded, and arbitrary messages are not accepted. The journal and progress survive server restarts because SQLite is the authority.

Operation results are validated by operation kind and capped at 64 KiB before persistence. Large or arbitrary stdout and stderr are not part of this milestone.

## Cancellation

`edge_operation_cancel` is idempotent for an already cancelled operation.

- Any queued operation can become `cancelled` before pickup.
- An interruptible leased operation records `cancel_requested=true`.
- The Edge observes that flag in the signed progress response, cancels its operation context and acknowledges the cancelled terminal state.
- A terminal completion update requires `cancel_requested=0`, so a late success cannot overwrite an accepted cancellation.
- Signed bundle update, rollback and repair operations are not interruptible after pickup because stopping the control operation alone would not reliably reverse the root-owned systemd effect.

If the Edge disappears after an accepted cancellation request, lease expiry closes the operation as `cancelled` during the next queue recovery pass.

## Recovery from another chat

`edge_operation_list` accepts a human Edge target alias and returns only active queued or leased operations. `edge_operation_status` reads one operation by its opaque operation id. Their output is limited to:

- operation id, kind and state;
- cancellation flag and whether cancellation is currently allowed;
- bounded progress;
- safe project alias and target when present;
- safe terminal reason;
- creation and update timestamps.

Device ids, workspace ids, local paths, request bodies, idempotency keys, credentials, raw output and result payloads are never returned by these lifecycle tools.

## Deliberate boundary

This milestone provides identity, idempotency, queueing, pickup, progress, terminal state, cancellation, bounded storage and restart recovery. Arbitrary argv, stdin, environment overlays, background processes and output streaming belong to the next roadmap milestone and are not smuggled into this lifecycle contract.
- Lifecycle responses expose `cancellable` so callers do not have to infer authority from the operation kind or state.
- `edge_operation_cancel` rejects a leased non-interruptible operation instead of pretending that the effect stopped.
