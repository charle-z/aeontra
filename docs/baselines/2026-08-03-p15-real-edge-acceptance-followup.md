# p15.0.19 real-Edge acceptance follow-up

Date: 2026-08-03

## Installed baseline

PR #139 merged at `52370dceb9bb6d829d8c7ab88e659239677047b8`. Official Edge
release workflow run `30786403458` published signed `p15.0.19`, and one normal stable
update installed it. Structured bundle/onboarding status and local doctor agree on a
valid compatible bundle, active managed service, one process, held lock, managed
coherence, matching release/commit, valid Bubblewrap/rootless components and no update
pending. The managed unit reports zero restarts and host load is normal.

## Real retry evidence

Hito 3B successfully started a new durable workload and read monotonically advancing
stdout/stderr cursors without duplicate blocks. That workload remains active so the
single restart acceptance gate is not repeated unnecessarily.

Hito 4 returned `project_toolbox_failed`, but host inspection proved that Podman 5.4.2
had pulled the fixed image, created the owner-labelled container, applied the requested
CPU/memory/PID limits, mounted only the workspace and rootless socket, and left the
container running. The saved record contains the canonical `sha256:` identity while
container inspect returns `.Image` as bare 64-character lowercase hexadecimal. The
ownership check compared those forms without normalization and rejected the healthy
container.

Direct Git status/fetch also failed closed without checkout mutation. Every externally
visible precondition passed: clean attached branch, exact owner-bound fetch/push
origin, expected upstream and reachable remote. A bounded temporary package diagnostic
showed the isolated runner corrupting local `rev-parse` output. Local commands pass an
empty credential, and Go `strings.ReplaceAll(output, "", "[REDACTED]")` inserts the
marker around every character. The diagnostic file was removed immediately and no
credential value was read or printed.

## Corrective candidate

- Parse the owner label and image identity separately, apply the existing strict
  Docker/Podman SHA-256 canonicalizer to the inspected container identity, then compare
  canonical values.
- Preserve local Git output verbatim when the credential is empty. Continue replacing
  every occurrence of a non-empty credential before returning command output.
- Retain fail-closed behavior for malformed image identities, ownership drift and all
  Git state/remote mismatches.

## Remaining acceptance

After exact-head CI, merge, signed release and one official update, reuse the existing
toolbox/process state. Prepare the toolbox service and persistent marker, perform one
operator restart, verify Hito 3B recovery/signal/stop/cleanup, verify Hito 4
rootfs/service/Podman persistence and cleanup, and repeat direct Git status/fetch plus
safe fast-forward behavior. Do not claim those gates complete before the real-device
results exist.
