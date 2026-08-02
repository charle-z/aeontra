# Direct Edge operation lifecycle

Direct GPT Web actions use the existing server-owned `edge_operations` journal. The
same lifecycle now carries bounded foreground execution and durable background process
control; these operations do not start or use a second OpenCode model runtime.

## Durable states

An operation moves through a closed state machine:

- `queued`: persisted and waiting for its paired Edge.
- `leased`: assigned to one Edge process under a signed, expiring lease.
- `succeeded`: completed with a validated bounded result.
- `failed`: completed with a stable safe code and no result body.
- `cancelled`: cancellation won before a terminal success or failure.

Terminal states are immutable. An expired normal lease returns to `queued`. An expired lease with cancellation requested becomes `cancelled` instead of being executed again. Status reads, active-operation listings, cancellation requests and idempotent retries normalize expired leases immediately; they do not wait for the Edge to poll again. Requeued attempts clear prior progress so the new lease can restart progress at revision `1`.

An authenticated completion whose result does not satisfy the bounded schema fails terminally as `operation_result_invalid`. The invalid payload is discarded instead of being persisted, and the lease is not requeued indefinitely. Invalid device, operation or lease identities remain rejected without changing state.

Root-owned bundle update, rollback and repair operations have an additional fail-closed recovery budget. They may recover through at most four leases and for at most twenty minutes from their first pickup. If either boundary is exhausted, the operation becomes `failed` with `operation_recovery_exhausted` instead of relaunching a privileged systemd effect indefinitely. Ordinary diagnostics, project operations and other interruptible work keep the normal requeue behavior.

Legacy rows are migrated transactionally. A bundle operation that was already `leased` receives one recorded attempt and its original creation time as the conservative first-pickup boundary, so an old restart loop cannot survive a server upgrade.

## Progress and restart recovery

While executing an operation, the Edge sends signed progress heartbeats. Progress contains only:

- a strictly increasing revision;
- a closed safe phase name;
- optional completed and total unit counters.

A valid progress heartbeat renews the lease for one bounded heartbeat window. Progress JSON is capped at 1 KiB, counters are bounded, and arbitrary messages are not accepted. The journal and progress survive server restarts because SQLite is the authority.

Operation results are validated by operation kind and capped at 64 KiB before
persistence. Foreground output is bounded in the operation result. Background output is
redacted before it reaches private Edge log files and is returned incrementally through
bounded status calls, so the control-plane journal never becomes an unbounded log store.

## Durable background processes

`project_process_start`, `project_process_status`, `project_process_stop`,
`project_process_signal`, `project_process_list`, and `project_process_cleanup` extend the
same trusted-workcell executor as `project_exec`:

- start accepts an argv array, relative workspace cwd, optional stdin and a non-secret
  environment overlay; it never adds an implicit shell;
- a caller idempotency key maps the same request to one opaque `pr_...` identity, while
  reuse with different parameters fails closed;
- private SQLite metadata and separate `0600` stdout/stderr files survive chat and
  control-plane reconnects;
- public results expose state, safe timestamps, bounded redacted output, offsets,
  truncation, exit status and terminal signal, but never PID, process group, host path,
  argv, environment or secret material;
- stop revalidates Linux PID start time and process-group identity before sending TERM,
  waits the requested bounded grace period, and sends KILL only if the owned group did
  not stop;
- terminal stop is idempotent and process rows have no TTL or automatic cleanup.
- each background Bubblewrap instance is owned by a minimal per-process worker from the
  same signed `mcp-edge` binary. The worker keeps redaction and wait/receipt handling
  alive independently of the control loop; the Edge systemd unit stops only its main
  process, so a signed restart or update does not implicitly stop managed groups;
- manager startup and every bounded list/cleanup recovery pass reconcile active rows
  against PID, Linux start ticks, process group and owner identity; reused, foreign,
  incomplete and disappeared identities become safe terminal states instead of being
  signalled;
- list returns at most 100 opaque lifecycle summaries and signal accepts only the
  closed `interrupt`, `terminate`, or `kill` enum;
- cleanup is explicit, individual or project-scoped, idempotent, and removes only
  terminal metadata and private logs. Live rows are counted and preserved.

Emergency concurrency and per-stream storage ceilings are administrator-controlled
Edge flags. They are protection against host exhaustion, not per-command allowlists or
process expiration.

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

This layer provides operation identity, queueing, cancellation, bounded result
transport and direct foreground/background execution with restart reconciliation.
It does not expose PID, host paths, arbitrary signals or a host-wide process list.
An explicit process stop is distinct from cancelling the short-lived Edge operation
which created or inspected it.
- Lifecycle responses expose `cancellable` so callers do not have to infer authority from the operation kind or state.
- `edge_operation_cancel` rejects a leased non-interruptible operation instead of pretending that the effect stopped.
