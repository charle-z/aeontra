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

## Source verification

- RED: the new focused matrix failed to compile because the guarded redaction helper
  did not exist; the toolbox ownership fixture also exercises the real bare Podman
  form.
- GREEN: the focused H3/H4/Git Edge matrix and documentation consistency tests pass.
- `go vet ./...`, `go build ./...` and `git diff --check` pass.
- The Windows-mounted full suite reaches only the documented NTFS executable-mode
  mismatch in `packaging/builder`. A clean ext4 clone of the exact candidate commit,
  preserving Git modes, passes `go test ./... -count=1`, `go vet ./...` and
  `go build ./...`.

## Remaining acceptance

After exact-head CI, merge, signed release and one official update, reuse the existing
toolbox/process state. Prepare the toolbox service and persistent marker, perform one
operator restart, verify Hito 3B recovery/signal/stop/cleanup, verify Hito 4
rootfs/service/Podman persistence and cleanup, and repeat direct Git status/fetch plus
safe fast-forward behavior. Do not claim those gates complete before the real-device
results exist.

## p15.0.20 restart finding

PR #140 merged at `4612fd80208717e9749174663c2995f612eaf56f`; all exact-head
checks passed, official workflow run `30788597266` published signed `p15.0.20`, and
one stable update installed it. The real Git retry then passed status/fetch as a clean
synchronized no-op. Hito 4 recovered the existing toolbox, persisted a marker and
installed tools, reached Podman 5.4.2 through the managed rootless socket and started a
durable service. The toolbox container survived the coordinated Edge restart.

Hito 3B retained monotonic non-duplicated output across the update, but the public
record became terminal with `process_identity_changed`. Cleanup removed the row, while
operator inspection proved both the exact old-release worker and its separate
Bubblewrap workload group were still alive. The operator verified both owner/group
identities, terminated only the workload group; the worker observed that exit and
terminated, leaving the managed Edge and Hito 4 toolbox untouched.

The final candidate treats private owner-only worker/child identities as an additional
liveness barrier. Reconciliation may repair a stale database identity only when the
private worker tuple revalidates as live. Cleanup revalidates the journal, worker and
child tuples and refuses removal while any exact identity remains active. It never
adopts a reused PID or an unowned/unsafe identity.

The exact candidate commit `f98f635d84080f26f885e8988192ed026423599b` passed
`go test ./... -count=1`, `go vet ./...`, `go build ./...` and `git diff --check` from
an isolated ext4 clone with Go 1.26.5. The mounted NTFS run passed every package except
the documented builder permission-mode contract (`0777` observed instead of `0755`);
the same package passed from ext4, so no production permission expectation was relaxed.

## p15.0.21 sandbox-leader finding

PR #141 merged at `7ce6ebd4e35b8c3325395155c30deb4f98c8a99a`; 16 exact-head
checks passed after one package job was rerun unchanged following an external GitHub
API HTTP 500. Official workflow run `30810209878` published signed `p15.0.21`, and one
stable update operation installed that exact release/commit. Production and the Edge
served the same merge; doctor remained ready with one process, held lock, managed
coherence, empty journal and `NRestarts=0`.

Git and Hito 4 are accepted from `p15.0.20` evidence. The toolbox retained its marker,
tools, rootless Podman socket and durable service across restart; repeated stop was
idempotent and exclusive cleanup removed only that toolbox and service.

The fresh Hito 3B process on `p15.0.21` recovered as `running` after one managed Edge
restart. Incremental stdout/stderr continued monotonically from 0 through more than
2,500 paired records without replay. A closed `interrupt` returned success but did not
stop the workload; bounded stop then failed. Host inspection proved Bubblewrap's outer
supervisor and its `--new-session` inner leader had different process groups. The
operator revalidated and sent TERM only to the exact inner test group. The worker then
wrote a stopped receipt, repeated stop was idempotent, exclusive cleanup succeeded,
no matching workload remained and doctor stayed ready.

Hito 3B therefore remains unaccepted. The next candidate consumes Bubblewrap's
server-owned `--info-fd`, persists the reported inner leader only after exact
PID/start-ticks/process-group/owner validation, signals that group and adds
`--die-with-parent` relative to the independent durable worker. This retains restart
survival while making a worker failure kill its sandbox instead of orphaning it.
