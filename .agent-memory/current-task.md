# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published exact-head baseline before this local cut: `ec81d203bfe32b52e398165cb6d57a6d2884b984`, 15/15 checks green.

## P16 Step 4 local candidate

The Edge development-workcell journal is upgraded to schema version 2 and now persists `started` before execution, `completed` before delivery, the original attempt, delivery lease, deterministic `jr_<sha256>` result identity and `delivered_at`. A lost completion response replays the same result before any new lease and never executes the task twice. A surviving `started` row enters manual reconciliation instead of blind retry. Delivered rows alone are eligible for bounded cleanup after seven days; pending evidence is retained. Capacity is bounded at 4096 entries.

`mcp-edge doctor` inspects `journal.db` read-only and reports `empty`, `ready`, `pending`, `reconciliation`, `migration_required` or `blocked` without exposing task/result identifiers. Unsafe ownership, mode, symlinks, size, integrity or schema fail closed.

Offline continuation is independent of the ChatGPT session but never exceeds control-plane authority. CLI and systemd now default to a ten-minute lease. The effective offline deadline is the earlier of the ten-minute grace and the last confirmed lease expiration minus a safety margin of up to 30 seconds. Successful heartbeats replace the authority deadline. The runner stops before the server may reassign the task. Pending delivery failure blocks the runner cycle before any new lease.

The workcell checks cancellation before every stage, so remote cancellation, STOP, timeout, offline-grace exhaustion or lease-authority exhaustion cannot start another stage. There is no silent VPS fallback.

## Validation completed locally

Green:

- all Go packages in bounded groups;
- focused Step 4, journal migration/layout/doctor and lost-response tests;
- tagged `p12_e2e` and `opencode_e2e` compile gates;
- `go vet ./...`;
- Staticcheck v0.7.0 with writable temporary caches;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`;
- documentation contract and systemd/CLI ten-minute lease guards.

No temporary helper files remain. Local race remains unavailable because CGO is disabled; exact-head CI is authoritative.

Next: update handoff/Brain, commit `Step 4: Add durable Edge job journal`, publish to PR #48, hold the SHA stable and require every exact-head gate green. Then continue Step 5 scheduler store without touching real Parrot, Coolify production or `main`.
