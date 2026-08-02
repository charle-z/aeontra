# Current task — direct Edge Hito 3B process recovery

## Verified remote base

- Branch: `codex/h3b-process-recovery`.
- Base: `origin/main` at `84bac0a13bf71078e94e407f49f52e5758f3b872`.
- Hito 3A code merged through PRs #123/#125 and is deployed behind the stable Front
  Door with 118 tools and catalog
  `sha256:d8b6b35f7f3dc7ef13a16d98130c0d129a50cf64f2e6216cecc39e3e1b1829ab`.
- Real Edge remains on signed `p15.0.12` at `b419d9dfd888a216813bca05aafa5d4de28f0196`.
  No later release or Edge update has been executed.

## Candidate scope

- Public `project_process_signal`, `project_process_list`, and
  `project_process_cleanup`; candidate catalog is 121 tools at
  `sha256:feca2e4d163cfcff7e08410d5d5b34a52396430d49515c0f601486ffde0b31e2`.
- Closed signals only: interrupt, terminate, kill.
- Bounded metadata-only list by project/target; no PID, argv, env or paths.
- Explicit individual/project cleanup removes terminal state only and preserves live
  processes/logs.
- Reconciliation on manager open and explicit lifecycle calls, without an idle poller.
- PID/start-ticks/group/current-owner validation and safe classification for missing,
  reused, foreign or incomplete state.
- A signed per-process worker owns Bubblewrap pipes, redaction and terminal receipts;
  it survives the Edge parent, while the service uses `KillMode=process`.
  Foreground commands and OpenCode retain their parent-death behavior.

## Verification

- Focused manager recovery, list, signal, cleanup, PID reuse, ownership and log
  corruption regressions are green in Parrot WSL.
- Edge operation and MCP catalog contracts are green after the 121-tool reconciliation.
- Full `go test ./... -count=1` passed every package except the known Windows DrvFS
  executable-mode fixture (`0777` observed, Linux contract `0755`); affected Linux
  package, Debian/Parrot packaging and docs suites are green.
- `go vet ./...`, `go build ./...`, focused CGO race for `internal/edge`,
  `internal/edgeclient`, and `internal/mcpserver`, catalog identity, and
  `git diff --check` are green.
- Commit, PR, exact-head CI, merge and production/Edge acceptance are pending.

## External release constraint

The roadmap authorizes ordinary releases generally but names only installed
`p15.0.12`. An attempted dispatch for inferred `p15.0.13` was rejected before execution
because that immutable version was not explicitly authorized. Do not bypass that gate
or claim real Hito 3A/3B Edge acceptance until an exact next release is authorized and
published through `.github/workflows/edge-release.yml`.

## Next exact action

Finish candidate documentation and adversarial tests, run the full local gates, review
the diff, commit and publish the Hito 3B PR. Continue all independent roadmap work while
the exact immutable release version remains the only external release blocker.
