# Current task — MCP redeploy continuity

## Status

- Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.
- Branch: `fix/mcp-redeploy-continuity`.
- Base: `main` / `origin/main` at `b3d2c160f3179da7966254406587520b735c61ea`.
- Current HEAD: `124b305076cf378762d94932f47569c218458353`.
- Tree clean after Hito 4 implementation.
- Hitos 0–4 completed. Next exact step: Hito 5 only.

## Hito 4 completed

Commit: `124b305076cf378762d94932f47569c218458353`.

Observability changes:

- Observability schema incremented to version 2.
- Every server process receives a random ephemeral `boot_id`; it is not a session id and carries no authority.
- Added closed event names for `server_drain_start`, `server_drain_end`, `mcp_session_created`, `mcp_session_reinitialized`, and `mcp_session_rejected`.
- Added closed error classes for authentication failure, missing/unknown/expired session, and draining server.
- HTTP, RPC, session and lifecycle events carry the same boot id for one process.
- Session lifecycle events include commit, tool count and current catalog hash.
- Recognized reinitializations are counted only when `initialize` explicitly carries a prior `Mcp-Session-Id`; the value is never stored or emitted.
- Drain start/end events include bounded duration and aggregate reconnect count.
- Authentication failures are distinguishable from transport failures.
- Unknown sessions are classified honestly; the server does not claim a stale catalog without external deployment evidence.

Security invariants:

- No session IDs, Authorization headers, cookies, OAuth payloads, IPs, paths, tool payloads or client identities are logged.
- No new free-form observability field or arbitrary attributes map was added.
- No authority, OAuth, jail, grant, Edge or workcell behavior changed.

Validation:

- `go test ./internal/observability ./internal/mcpserver ./internal/app ./docs -count=1`: green.
- `go vet ./internal/observability ./internal/mcpserver ./internal/app`: green.
- `git diff --check`: green.
- Focused tests prove session/reconnect/auth/drain classifications and absence of identifier or credential leakage.

## Next exact step

Hito 5 — add the full two-instance replacement E2E, including readiness, old/new sessions, safe tool call, durable Brain state, same-contract hash stability and changed-contract hash transition. Integrate it into existing CI without creating a duplicate workflow. Do not begin Hito 6 until Hito 5 has implementation, all focused and affected tests green, diff review, local commit, Brain update, memory update and a clean tree.
