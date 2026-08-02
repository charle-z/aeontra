# Current task — direct Edge Hito 3A background processes

## Verified base

- Feature branch: `codex/h3a-background-processes`.
- Base: `origin/main` at `b419d9dfd888a216813bca05aafa5d4de28f0196`.
- Stable source release and installed Edge before this candidate: `p15.0.12` at the
  same base commit.
- Public backend and official Front Door were healthy on the base contract before
  implementation. Live identity must be queried again before rollout.

## Candidate scope

- Public tools: `project_process_start`, `project_process_status`,
  `project_process_stop`.
- Same trusted-workcell executor and Bubblewrap construction as `project_exec`.
- Private Edge SQLite metadata and separate bounded redacted stdout/stderr logs.
- Opaque durable process identity; no PID, argv, environment or host paths publicly.
- Start idempotency, conflicting-request rejection, closed states, incremental reads,
  natural exit, process-group TERM/KILL and PID start-time reuse defense.
- Emergency concurrency and per-stream storage ceilings; no process TTL or automatic
  cleanup.

## Verification

- Focused Edge/client/MCP/command suite: green in Parrot WSL2 Go 1.26.5.
- Catalog/app/integration/docs suite: green.
- Focused CGO race suite for `internal/edge`, `internal/edgeclient` and
  `internal/mcpserver`: green.
- Split-write token and multi-line private-key redaction is tested before persistence.
- The full suite passed every package except the known DrvFS-only executable-mode
  assertion in `packaging/builder` (`0777` observed instead of Linux `0755`); Linux CI
  remains authoritative for that host-specific fixture. Vet and build passed; the final
  WSL `git diff` command cannot traverse the Windows gitdir, while native Git
  `diff --check` is green.
- Exact-head CI, merge, deployment/catalog reconciliation, signed release, Edge update
  and real-device process acceptance have not occurred yet.

## Next safe action

Review the complete diff, commit the candidate, publish a normal
PR and require all exact-head checks. Do not claim Hito 3A closed before live release
and real Edge start/status/output/stop/no-orphan evidence.
