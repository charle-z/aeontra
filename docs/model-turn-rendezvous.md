# Durable model-turn rendezvous

The model-turn rendezvous is the provider-neutral boundary between an agent runtime
and an external model. The default production layout is `/state/model-turns/`; tests
and isolated runners may supply another private absolute root.

## State machine

```text
created -> awaiting_model -> responded -> consumed
                  |              |
                  |              +-> failed
                  +-> disconnected -> awaiting_model
                  +-> cancelled
                  +-> expired
                  +-> failed
```

A turn is identified by an opaque `mt_` id and a monotonically increasing sequence
within one `runtime_id`. Creation performs compare-and-swap against the last durable
sequence. Responses must repeat the exact `runtime_id`, `turn_id`, expected sequence,
and canonical request digest. A turn accepts one response and one consumption only.
Replay, late response, duplicate sequence, sequence gaps, and invented tool ids fail
closed. There is no automatic model or provider fallback.

## Durable metadata

The durable public record contains exactly:

- `runtime_id`
- `turn_id`
- `sequence`
- `request_digest`
- `request_ref`
- `response_digest`
- `response_ref`
- `status`
- `created_at`
- `expires_at`
- `responded_at`
- `consumed_at`

Canonical request and response bodies live separately in the private bounded body
result store, addressed only by opaque `mb_` references. The metadata row, audit
stream, and structured observability stream do not contain prompts, messages, tool
arguments, tool results, model reasoning, or body excerpts. Offered tool ids are
stored as closed internal validation metadata and are not accepted from a response
unless they were present on the original request.

## Durability and bounds

The SQLite database is private (`0600`) under a private (`0700`) root with symlink
ancestors rejected. WAL plus `synchronous=FULL` provides restart durability. Default
turn TTL is 15 minutes, maximum TTL is one hour, default body quota is 64 MiB, and
maximum configurable quota is 256 MiB. Active bodies are never evicted to admit a new
body; quota pressure fails the operation. Expired bodies are deleted and terminal
turn bodies may be evicted oldest-first.

A runtime interrupted in `awaiting_model` may mark the turn `disconnected`. After a
process restart, `ResumeRuntime` moves unexpired disconnected turns back to
`awaiting_model` without changing turn id, sequence, digest, request ref, or body.

For a remote-Edge runtime before its first turn, inability to read the private runtime
goal is handled as a retryable lease delivery failure. The runtime returns to
`awaiting_edge` and the Edge repeats the same signed receipt; the control plane must
not turn that storage/availability condition into a terminal Edge failure.
