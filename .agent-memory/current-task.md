# Current task — MCP redeploy continuity

## Status

- Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.
- Branch: `fix/mcp-redeploy-continuity`.
- Base validated: `main` / `origin/main` at `b3d2c160f3179da7966254406587520b735c61ea`.
- Current implementation HEAD before this continuity commit: `6ad667f1148f0bc226db3ab83169e176b1e9a08e`.
- Hitos 0–5 complete.
- Hito 6 local preparation complete; next exact action is publish branch and create PR.

## Hito 6 local corrections

Commit: `6ad667f1148f0bc226db3ab83169e176b1e9a08e`.

- `cmd/brain-smoke` now performs `initialize`, captures the returned `Mcp-Session-Id`, and reuses it for `brain_index` and `brain_context`.
- The smoke still prints no credential, note content, session id or private path.
- Removed the now-unused non-observed session validation wrapper found by Staticcheck.
- No server authority or public tool contract changed.

## Final local release gates

All executed against the final implementation state and green:

- `go test ./... -count=1`.
- `go vet ./...`.
- `go build ./...`.
- Staticcheck v0.7.0 over `./...` with a private temporary cache.
- Actionlint v1.7.12.
- `git diff --check`.
- Complete `internal/mcpserver` suite covering HTTP transport, `/mcp`, sessions, OAuth integration, console boundaries, catalog, Brain, observability, drain and redeploy E2E.
- `internal/oauth`, `internal/console`, `internal/brain`, `internal/observability`.
- `cmd/brain-smoke`, `cmd/console-smoke`, `cmd/mcp-catalog-smoke`.
- `packaging/...`, `internal/app`, `internal/integration`.
- Catalog identity computed twice with identical result: `sha256:1d3646af205ec2b1a01a47d034641ac4cb8a4843d9c7879b122432308e961007`.
- Tool count remains 102.

Initial full-suite failure and resolution:

- The first `go test ./...` found `cmd/brain-smoke` calling Brain tools without a session, returning HTTP 400.
- This was corrected by using the protocol-required initialize/session flow.
- The second and final full suite passed.
- Staticcheck initially could not create its cache due the runtime home being non-writable; it was rerun with an isolated private temporary cache and found one unused wrapper, which was removed. The final Staticcheck run passed.

## Security and authority confirmation

- `/mcp` and `/console` authentication boundaries remain intact.
- No token, cookie, OAuth payload or session identifier is persisted or logged.
- No free shell was added.
- Jails, plans, grants, audit, Edge and workcells were not weakened.
- Old sessions lose authority at replacement; new sessions require successful initialize.
- Catalog identity changes only for public contract changes.

## Next exact actions

1. Publish `fix/mcp-redeploy-continuity` without force.
2. Create a PR against `main` with diagnosis, prior behavior, solution, server/client limits, tests, catalog impact, risks and production plan.
3. Wait for all exact-head checks; correct only demonstrated failures.
4. Before merge/deploy, update Brain and `.agent-memory` with PR number, exact head, checks, expected merge and recovery instructions. Any new memory commit must be pushed and revalidated by CI.
5. Merge using a merge commit only when all checks are green.
6. Synchronize clean local `main`.
7. Deploy only Coolify app `jqf7qz5ensoqtvl1tb197gcv` normally, not without cache.
8. Verify production `/`, `/version`, protected `/mcp`, protected `/console`, new MCP session, old-session fixture, persistent Brain and active-conversation behavior.
