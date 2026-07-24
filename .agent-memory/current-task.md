# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Step 4 head `b6765dfcd681a85171157a326b01e01a2ab3515c` passed 15/15 exact-head checks.

## P16 Step 5 local candidate

Added private `internal/workqueue` SQLite schema version 1 for later admission and workers. It is coordination only and exposes no public MCP tools or execution authority.

Contract:

- exactly one active control-plane writer through an advisory lock that releases on process exit;
- unsupported multi-writer configuration and controller identity changes fail closed;
- typed `blocked`, `queued`, `leased`, `succeeded`, `failed`, `cancelled` states and legal transition guard;
- globally unique idempotency keys, immutable workspace/pool/profile/payload hash and order-independent dependency set;
- default bounds: 1024 total jobs, 64 per workspace, 16 dependencies, 100 list rows and 64 MiB SQLite budget;
- concurrent identical enqueue produces one job; different concurrent enqueue remains within global/workspace bounds;
- leases increment attempt and fencing token; stale heartbeat/completion is rejected;
- expired lease recovery and queued/running cancellation;
- dependency success promotes blocked jobs; failed/cancelled dependencies propagate `dependency_failed` transitively;
- queue payload contains only metadata/hash, never source, prompt, commands, credentials or paths;
- private bounded backup and fail-closed restore fixture with integrity/schema revalidation.

Documentation: `docs/workqueue-store.md`; checklist Step 5 is closed locally.

## Local validation

Green:

- all Go packages in bounded groups;
- focused workqueue tests, concurrency, backup/restore, fencing, dependencies and security tests;
- `go vet ./...`;
- Staticcheck v0.7.0 with writable temporary caches;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`.

Race cannot run locally because CGO is disabled; exact-head CI remains authoritative. No temporary helper files remain.

Next: update handoff/Brain, commit `Step 5: Add durable scheduler store`, publish to PR #48 and hold the SHA until all checks are terminal. Then start Step 6 admission/fairness test-first.
