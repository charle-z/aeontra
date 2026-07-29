# Edge single-process and authoritative doctor contract

Each resident `mcp-edge opencode` process must exclusively own one private state root.
The process acquires `edge-instance.lock` before it opens runtime registries, starts
supervisors, polls control operations, or accepts runtime work.

The lock is a kernel `flock` on a private regular file inside the state root. Its bounded
metadata records the owner PID, the process start time from `/proc`, the signed release
and commit loaded by that process, an optional systemd invocation identifier, and the
acquisition time. PID plus process start time prevents PID reuse from being mistaken for
the original owner.

## Required behavior

- The first process for a state root acquires the lock.
- A second process for the same state root fails immediately with
  `instance_lock_occupied`.
- Different private state roots remain isolated and may run concurrently.
- A clean exit releases the kernel lock.
- A process death releases the kernel lock without manual cleanup.
- A separate file descriptor or process cannot unlock another owner's lock.
- The implementation never searches for processes by name and never kills a process.
- An obsolete unlocked lock file is `stale_recoverable` and is replaced during the next
  valid acquisition.
- Existing state roots without the lock file remain compatible; the first updated
  process creates it.

## Authoritative diagnosis

`mcp-edge doctor` cross-checks the fixed systemd service, the lock owner, PID start
identity, and the release and commit loaded by the active process. A ready managed
process reports a bounded line containing values equivalent to:

```text
service=active process=single lock=held coherence=managed release=<SIGNED_RELEASE> commit=<SIGNED_COMMIT>
```

A controlled manual process may be healthy with:

```text
service=inactive process=single lock=held coherence=manual
```

The process and lock remain authoritative when the active owner carries valid systemd
invocation metadata but a restricted environment cannot query `systemctl`. This avoids
the former false `edge_service_inactive` diagnosis.

Closed process states are `inactive`, `single`, `duplicate`, and `incoherent`. Closed
coherence states are `stopped`, `managed`, `manual`, `duplicate`, and `incoherent`.
The doctor exposes bounded lock states including `missing`, `held`,
`stale_recoverable`, and explicit incoherent states. It never exposes filesystem paths,
keys, commands, or opaque runtime/workspace identifiers.

A systemd main PID that differs from the verified lock owner is diagnosed as
`process=duplicate coherence=duplicate`. A service without a held lock, an invalid held
lock, or a live metadata owner without the kernel lock is diagnosed as incoherent. No
automatic process termination or lock deletion occurs.
