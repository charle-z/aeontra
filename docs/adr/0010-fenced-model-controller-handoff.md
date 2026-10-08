# ADR 0010 — Fenced model-controller handoff

Status: Accepted for implementation

Date: 2026-10-07

## Decision

An optional controller record in the existing model-turn store coordinates which
client supplies model responses to one runtime. Existing uncontrolled runtimes remain
unchanged. The caller chooses an opaque controller identity; it is a coordination ID,
not a credential, chat identifier or new authorization principal. The authenticated
single owner retains the existing authority.

Claim binds a fresh pending turn. Prepare pauses response admission; the successor
ACKs that exact turn, sequence and request digest. Transfer changes controller and
increments generation atomically. Responses from the previous generation cannot be
accepted. Release acknowledges the predecessor after transfer; the new controller
can already respond, but another handoff waits for that acknowledgement. If the
successor disappears before transfer, abort restores the original controller at a new
generation. There is no automatic takeover on a timeout.

The same stock worker, runtime, workspace and fenced worktree remain in place.
Handoff does not create a second writer, stop background processes, transfer filesystem
authority, open a ChatGPT chat or retry an external effect. Pending durable effects
must still be reconciled by their existing identities before continuation.

The transition and response admission share the model-turn SQLite transaction.
Only a current unexpired pending turn on a live runtime may be claimed or transferred;
a responded/consumed batch is never handed to another client for replay. Controller
state survives restart and a lost acknowledgement can be reconciled through runtime
status. A SQLite trigger also rejects an old binary's unfenced response UPDATE on a
controlled runtime. Normal legacy responses have no controller requirement.

The additive migration adds response-admission columns, a controller table and trigger
inside one transaction. The table is bounded to 4096 controller records. Preserve the
private model-turn store in the consistent pre-rollout backup; a downgrade cannot
continue a controlled runtime without the new contract, although unchanged runtimes
remain compatible. Do not remove the trigger to resume an old controller.

## Validation

Tests cover two claimants, restart during prepare, absent/wrong successor, stale ACK,
abort fencing, transfer replay, outstanding release, old-owner responses, terminal and
expired runtime rejection, old SQL writer rejection, and unchanged legacy responses.
Source, deployed backend, hydrated client schemas and real-worker acceptance remain
separate gates.
