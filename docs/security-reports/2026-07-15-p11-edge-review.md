# P11 Edge final security review

Date: 2026-07-15. Scope: `origin/main...codex/p11-edge-core` through implementation
head `3a441e6`. This review makes no deployment or pairing claim.

## Reviewed boundaries

- `/edge/v1/pair` and signed `/edge/v1/tasks/*` routing;
- pairing entropy, hashing, expiry and one-time consumption;
- Ed25519 canonical signatures, timestamp skew and persistent nonce anti-replay;
- device revocation and its effect on subsequent heartbeat/authentication;
- lease ownership, expiration, cancellation, idempotency and exact completion replay;
- redaction before persistence in audit, result store, task summaries and journals;
- SQLite page limits, TTL pruning and fixed-segment log rotation;
- Bubblewrap mounts/network and local planner authority;
- systemd user, capabilities, writable paths, namespace requirement and kill switch.

## Findings

No critical or high-severity finding remains open.

The public pairing route is deliberately unauthenticated but accepts only a random
192-bit code that is stored as a digest, expires within ten minutes, is atomically
consumed once, and returns a generic failure. TLS remains mandatory at the client.
Signed routes reject unknown devices, revoked devices, stale timestamps, malformed
signatures and a reused nonce before a task handler runs. Bodies are bounded and
strict JSON rejects unknown or trailing values.

Task creation accepts no shell or argv. The only objective is `validate`; workspace,
duration, output, network policy, holder, result reference and acceptance criteria are
closed and bounded. The initial workcell rejects even the reserved registry policy.
Per-device idempotency is unique in SQLite. Exact lease ownership is revalidated on
heartbeat and completion, and conflicting terminal replay is rejected.

Secret-shaped objective/result content is rejected or redacted before persistence.
The result store persists redacted bodies only; audit redacts arguments, errors and
file fields; telemetry is content-free. Rotation and quota failures fail closed rather
than silently widening retention.

Bubblewrap receives locally selected fixed validation stages, not VPS-provided
commands. `--unshare-all` removes network access; only `/workspace` is writable;
`/mnt/c`, `/mnt/d`, symlink roots, permissive roots, the host home, Edge private state
and `/var/run/docker.sock` are absent. Package scripts can execute repository code,
but only inside this no-network workcell boundary.

The systemd service is non-root, has an empty capability set, private devices/tmp,
strict system protection and only two writable roots. `RestrictNamespaces=false` is
an explicit residual requirement because unprivileged Bubblewrap must create its own
namespaces; removing it would disable the isolation mechanism. This exception does
not grant remote commands and is documented for later profile-specific hardening.

## Residual risks and release conditions

- A paired Edge is powerful enough to execute repository validation scripts inside
  its workspace. Pairing therefore remains an explicit human action after deployment.
- Availability depends on SQLite and local clock health; failures stop work rather
  than bypassing authentication or idempotency.
- The client has outbound HTTPS control-plane access. Workcell child processes have
  no network; registry-only egress is deliberately deferred.
- WSL/Bubblewrap behavior still requires the documented human installation test.
- Merge, deploy, pairing, WSL installation, Parrot/security profiles and broader
  execution authority remain separate approvals.
