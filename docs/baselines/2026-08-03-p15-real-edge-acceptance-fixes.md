# p15.0.18 real-Edge acceptance corrections

Date: 2026-08-03

This baseline records the first real-device acceptance pass after the clean signed
`p15.0.18` lifecycle handoff. The complete bounded operation ledger remains in Brain
note `p15-0-18-parrot-trusted-linux-real-acceptance-2026-08-03`.

## Observed device state

- The managed templated Edge is active with one process, one held lock, managed
  coherence, `NRestarts=0`, release `p15.0.18` and the exact production commit.
- The unpackaged legacy unit remains inactive and disabled. The onboarding path is
  active and enabled.
- Rootless Podman 5.4.2 is available over the user-owned socket with cgroup-v2 CPU,
  memory and PID controllers. Host load returned to normal.
- Hito 3A passed real start, idempotent reuse, incremental split output, repeated stop
  and explicit cleanup.

## Failures found by real acceptance

Hito 3B exposed a worker/control split that the fake platform tests did not reproduce.
The durable worker survived an Edge restart as designed, but signal requests were
relayed through the worker while the Bubblewrap workload owned a separate process
group. A kill request could consume its private control record without producing a
terminal receipt, leaving the durable row in `stopping`. The operator terminated only
that verified test workload group and restarted only the managed Edge. Doctor returned
ready with one managed process, held lock and an empty lifecycle journal.

Hito 4 failed before container creation. The fixed Debian image was present and valid,
but Podman 5.4.2 returned `image inspect --format {{.Id}}` as 64 lowercase hexadecimal
characters without the `sha256:` prefix. The manager accepted only the Docker-shaped
prefixed form and therefore classified a healthy engine as unavailable.

Git synchronization failed closed without changing the checkout. Independent local
inspection proved a clean attached `main`, exact owner-bound origin/upstream and a live
remote at the same commit. The normal `gh` login was reimported through
`mcp-edge github import-gh` without exposing the credential, and the managed Edge was
restarted once. Successful direct Git status/fetch remains a release acceptance item.

## Candidate correction

- A worker now records a private owner-only identity for the actual workload process
  group before announcing readiness. Direct signals revalidate that identity and
  target the workload group, leaving the worker alive to write the terminal receipt.
  A closed relay remains only for workers created by an older signed release.
- Toolbox image identity normalization accepts exactly either 64 lowercase hexadecimal
  characters or `sha256:` followed by the same digest and stores the canonical prefixed
  representation. All other algorithms, lengths and uppercase forms fail closed.

## Verification

- Focused Edge matrix:
  `go test ./internal/edge ./internal/edgeclient ./internal/mcpserver ./cmd/mcp-edge -count=1`
  passed.
- The Windows-mounted worktree reported the expected non-reproducible NTFS executable
  mode mismatch in `packaging/builder`. Repeating from an ext4 copy with exact Git file
  modes passed `go test ./... -count=1`, `go vet ./...` and `go build ./...`.
- Real-device Hito 3B, Hito 4 and successful Git synchronization remain pending until
  this correction passes exact-head CI, merges, is published in the next signed Edge
  release and is installed through the normal closed updater.
