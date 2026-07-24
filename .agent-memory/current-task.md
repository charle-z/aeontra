# Current task

Historical deployed baseline remains unchanged: the canonical VPS `main` worktree and production are synchronized at `0daeb5df0d5e61c1b33aeb363c10aeb0ea91ddf0`; this P16 cut has not changed `main`, Coolify, production or Parrot.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Step 4 head `b6765dfcd681a85171157a326b01e01a2ab3515c` passed 15/15 exact-head checks. Initial Step 5 head `0c7a9f028667ba2059f006165a3d8f979aa779fd` passed 14/15; `Verify` failed only because the real OpenCode provider restart integration observed five socket wait attempts while its timing-sensitive test allowed at most four. The safety contract itself passed: one turn creation, one immutable response path, successful restart and no duplicate work.

## P16 Step 5 hardening candidate

The published private `internal/workqueue` SQLite scheduler store remains coordination-only and exposes no public MCP execution authority. The local follow-up hardens it before Step 6:

- rejects future schemas and non-empty unknown schema-zero databases before adopting or creating current tables;
- commits expired-lease recovery even when no replacement job is available;
- converts an expired cancelled lease into a valid terminal `cancelled` row with a safe summary while retaining historical attempt/fence evidence;
- validates queued, blocked, leased and terminal cancellation/reason combinations semantically;
- validates every terminal result with the same length, result-ref and secret-material rules used at completion;
- proves advisory writer-lock release and durable job persistence across a real subprocess exit;
- covers unknown schema adoption, cancelled lease expiry and process crash recovery;
- adds `internal/workqueue` to the blocking atomic coverage gate at 70%; measured final local coverage is 74.3%;
- stabilizes the real provider restart test by removing only the scheduler-dependent maximum socket-attempt count while preserving one-create, at-least-one-retry and exact-same-path invariants. The focused restart test passed ten consecutive executions.

## Final local validation

Green on the current working tree:

- exact `go test ./... -coverprofile=coverage.out -covermode=atomic -count=1`;
- all 15 package-specific coverage thresholds, including workqueue 74.3% >= 70%;
- focused workqueue hardening and documentation tests;
- provider restart integration repeated ten times;
- `go vet ./...`;
- Staticcheck v0.7.0 with writable temporary caches;
- `go build ./...`;
- Actionlint v1.7.12;
- Windows/amd64 cross-compilation of `internal/workqueue`;
- `git diff --check`.

The coverage artifact and all temporary helpers were removed. Local CGO race remains unavailable; the new exact-head CI Race job is blocking.

Next: commit `Step 5: Harden scheduler store invariants`, publish to PR #48, hold the exact SHA until all 15 checks are terminal, diagnose any red from logs, then begin Step 6 admission/fairness test-first only after the hardening head is fully green.
