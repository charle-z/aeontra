# Current task — MCP redeploy continuity

## Status

- Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.
- Branch: `fix/mcp-redeploy-continuity`.
- Base: `main` / `origin/main` at `b3d2c160f3179da7966254406587520b735c61ea`.
- Current HEAD: `023fb10a5b043b87431219b33f67088bdbc8d4f9`.
- Tree clean after Hito 3 implementation.
- Hitos 0, 1, 2 and 3 completed. Next exact step: Hito 4 only.

## Hito 3 completed

Commit: `023fb10a5b043b87431219b33f67088bdbc8d4f9`.

Files changed include `internal/mcpserver/catalog.go`, `server.go`, `http.go`, catalog/session/integration tests, catalog baselines, and `docs/runbooks/catalog-cache.md`.

Decisions and results:

- Catalog identity now includes public tool descriptions because they alter model-visible semantics.
- Name, description, version, input schema, defaults, annotations and membership are contractual.
- Handlers and process-local operational data are excluded.
- `tools/list` is name-sorted and deterministic, independent of registration order.
- The production catalog is immutable for one process lifetime.
- Server advertises `tools.listChanged=false`; it no longer emits a false notification merely because a container restarted.
- Real contract changes are discovered by a fresh session and `tools/list` on the replacement instance.
- Tool count remains 102.
- New deterministic catalog hash: `sha256:1d3646af205ec2b1a01a47d034641ac4cb8a4843d9c7879b122432308e961007`.

Validation:

- `internal/mcpserver` complete in bounded A-H, I-P, Q-Z groups: green.
- `cmd/mcp-catalog-smoke`, `internal/app`, `internal/integration`, `docs`: green.
- Focused catalog determinism, operational-state exclusion, contractual-change, stable-order, SSE and `/version` tests: green.
- `go vet` on affected packages: green.
- `git diff --check`: green.

Risks preserved:

- Do not confuse a stale ChatGPT conversation with server failure.
- Do not add mutable in-process catalog machinery merely to emit notifications.
- Do not persist session credentials or expand authority.

## Next exact step

Hito 4 — add bounded, non-sensitive reconnection observability. Do not start Hito 5 until Hito 4 has implementation, focused tests, affected suites, diff review, local commit, Brain update, memory update and a clean tree.
