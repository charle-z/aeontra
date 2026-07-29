# Current task — MCP redeploy continuity

Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.

## Completed milestones

### Hito 0 — controlled reproduction

- Branch: `fix/mcp-redeploy-continuity`.
- Base: `main` / `origin/main` / merge-base `b3d2c160f3179da7966254406587520b735c61ea`.
- Reproduction commit: `6fa43f0bd220bc275e162ae91d242b9642068ea1`.
- Continuity commit: `e16ba02f4cd8c3d7119d6444f403e17e3653c6a7`.
- Defect: the HTTP transport returns a process session header on `initialize` but ignores the client's later `Mcp-Session-Id`; an old instance session is accepted by a replacement instance, including against a changed test catalog.
- Tests: focused redeploy characterization and complete `internal/mcpserver` suite passed.

### Hito 1 — bounded graceful drain

- Implementation commit: `55a4b36e7cccf0376c5d1f87550b8ef5632b209f`.
- Files changed: `Dockerfile`, `docs/deploy-coolify.md`, `internal/mcpserver/client_capabilities.go`, `internal/mcpserver/http.go`, `internal/mcpserver/http_lifecycle.go`, `internal/mcpserver/http_lifecycle_test.go`, `internal/mcpserver/http_listener.go`, `internal/workflowpolicy/security_remediation_test.go`.
- Added `/readyz` while preserving `/healthz` as liveness.
- Docker/Coolify healthcheck now uses `/readyz`.
- `SIGTERM`/context cancellation marks the instance draining and not-ready before listener shutdown.
- New `initialize` requests receive `503` with `Retry-After` during drain.
- Active requests may complete inside an 8-second bounded shutdown window.
- SSE streams close when drain starts so shutdown is not held open indefinitely.
- On deadline, active connections are force-closed and the transport returns a clear drain deadline error.
- Process-local client/session capability metadata is invalidated only after active work completes or the deadline forces closure, so in-flight requests are not deprived of their metadata.
- `/mcp` remains unauthorized without credentials; `/console/status` remains unauthorized without a console session.
- Persistent stores were not moved or rewritten.

## Hito 1 verification

- `go test ./internal/mcpserver -run TestHTTP -count=1` — pass.
- `go test ./internal/mcpserver ./internal/app ./internal/workflowpolicy ./docs -count=1` — pass.
- `git diff --check` — pass.
- Focused race run was attempted but the local executor has CGO disabled; race is not an Hito 1 acceptance gate and remains covered by the existing CI Race job.
- Diff reviewed after moving metadata invalidation to the end of the drain window.
- Tree clean after commit.

## Boundaries preserved

- No URL, domain, OAuth configuration, authority mode, jail, grant, Edge, workcell, secret handling, or general shell changed.
- The unrelated `h1-edge-runtime-observability` worktree/branch was not touched.
- ChatGPT connector refresh inside an active conversation remains a client behavior, not a server guarantee.

## Exact next step

Hito 2 only: introduce real in-memory HTTP session registration and validation. A successful `initialize` must create a fresh opaque session ID; non-initialize MCP requests must require that current-instance session. Missing, unknown, expired, or previous-instance headers must fail clearly without `tools/list`, `tools/call`, or authority fallback. A new session on the replacement instance must complete `initialize` → `tools/list` → safe `tools/call` with the same URL and OAuth. Do not begin catalog work from Hito 3 until Hito 2 is committed, tested, reviewed, and recorded.
