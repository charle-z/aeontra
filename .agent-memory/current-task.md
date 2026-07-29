# Current task — MCP redeploy continuity

## Status

- Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.
- Branch: `fix/mcp-redeploy-continuity`.
- Base: `main` / `origin/main` at `b3d2c160f3179da7966254406587520b735c61ea`.
- Current HEAD: `6e2f23a8d2bb4474e226b264c1a8eef297a7c06d`.
- Tree clean after Hito 5 implementation.
- Hitos 0–5 completed. Next exact step: Hito 6 only.

## Hito 5 completed

Commit: `6e2f23a8d2bb4474e226b264c1a8eef297a7c06d`.

E2E replacement guarantee:

- Uses two successive MCP server instances behind one stable logical HTTP endpoint.
- Server A starts ready, initializes a session, lists tools and reads a safe Brain fixture.
- A enters drain; `/readyz` changes to 503 and new initialize requests are rejected.
- Brain A closes cleanly before B reopens the same persistent Brain root.
- Server B has a distinct ephemeral boot id and is checked ready before becoming the active logical endpoint.
- The old A session is rejected for both `tools/list` and `tools/call` and receives no replacement authority.
- A fresh B session is created using the same endpoint and credentials.
- The new session obtains `tools/list` and executes a safe tool.
- The Brain note written before replacement is readable through B after reopening the shared store.
- Same-contract A/B instances preserve the exact catalog hash.
- A controlled contractual tool addition produces a new deterministic hash and is visible/callable only through the new session.

CI integration:

- Added a focused `Verify MCP instance replacement continuity` step to the existing `.github/workflows/ci.yml` verify job.
- No duplicate workflow was created.
- The normal complete `go test ./...` CI gate still runs afterward.

Validation:

- Focused `TestRedeployE2E...` same/changed contract subtests: green.
- `go test ./internal/mcpserver ./internal/brain ./internal/tools ./internal/integration ./docs -count=1`: green.
- Affected-package vet: green.
- Actionlint v1.7.12: green.
- `git diff --check`: green.

Security and authority:

- The Brain root remains outside repository policy roots.
- No token, session id, OAuth payload or external infrastructure is persisted by the test.
- No authority, jail, grant, Edge or workcell behavior changed.

## Next exact step

Hito 6 — run all local release gates, update continuity, publish the branch, create the PR, wait for exact-head checks, correct only real failures, update Brain and `.agent-memory` before deployment, merge with a merge commit, synchronize clean `main`, deploy only the registered MCP Devbox production application normally, and verify production routes, exact merge identity, new session, old session fixture and active-conversation behavior.
