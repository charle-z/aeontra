# Current task — MCP redeploy continuity

Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.

## Completed milestones

### Hito 0 — controlled reproduction

- Branch: `fix/mcp-redeploy-continuity`.
- Base: `main` / `origin/main` / merge-base `b3d2c160f3179da7966254406587520b735c61ea`.
- Reproduction commit: `6fa43f0bd220bc275e162ae91d242b9642068ea1`.
- Continuity commit: `e16ba02f4cd8c3d7119d6444f403e17e3653c6a7`.
- Defect reproduced: a replacement instance accepted an old `Mcp-Session-Id` and allowed `tools/list` and `tools/call`, including against a changed test catalog.

### Hito 1 — bounded graceful drain

- Implementation commit: `55a4b36e7cccf0376c5d1f87550b8ef5632b209f`.
- Continuity commit: `84765b8fdb54fa5618914bfdb7e6064610a710bf`.
- `/readyz` is separate from `/healthz`; container healthcheck uses readiness.
- Drain rejects new initialize requests, closes SSE, allows active work for 8 seconds, force-closes on deadline, and invalidates process-local metadata after the drain window.

### Hito 2 — instance-bound reinitalization

- Implementation commit: `4b280c4af8c4afc28fb3cee7d9943ae716784418`.
- Added bounded opaque process-local HTTP sessions with 24-hour sliding idle expiry and a 4096-session cap.
- Successful `initialize` always creates a fresh session ID, even when the request carries a previous-instance header.
- Later authenticated `GET`, `POST`, and `DELETE /mcp` require a current-instance `Mcp-Session-Id`.
- Missing session returns protocol JSON error with HTTP 400.
- Unknown, deleted, expired, or previous-instance sessions return protocol JSON error with HTTP 404, expose no replacement identifier, and do not reach tools.
- `DELETE /mcp` explicitly revokes the session and its process-local client capability metadata.
- Session IDs are never persisted and are reset after the bounded drain finishes.
- OAuth credentials remain valid across a replacement: the same token can initialize a fresh session on the same logical endpoint; the old OAuth session is rejected and the new one can list tools.
- The same-catalog and changed-catalog replacement tests now require old-session rejection and new-session success.
- `docs/connect-remote.md` documents session reuse, reinitialization after 404, DELETE, and reverse-proxy header preservation.

## Hito 2 verification

- `go test ./internal/mcpserver -run '^Test[A-H]' -count=1` — pass.
- `go test ./internal/mcpserver -run '^Test[I-P]' -count=1` — pass after updating the payload counter test to initialize a session.
- `go test ./internal/mcpserver -run '^Test[Q-Z]' -count=1` — pass after updating the SSE catalog test to use a session.
- `go test ./internal/app ./docs -count=1` — pass.
- `go vet ./internal/mcpserver ./internal/app` — pass.
- `git diff --check` — pass.
- Diff reviewed; tree clean after implementation commit.

## Preserved boundaries

- No URL, domain, OAuth configuration, credentials, authority mode, jail, grant, Edge, workcell, secret handling, or general shell changed.
- Session state is process-local and does not become a durable credential.
- The unrelated `h1-edge-runtime-observability` worktree/branch was not touched.
- ChatGPT connector refresh inside an active conversation remains a client behavior, not a server guarantee.

## Exact next step

Hito 3 only: validate and correct deterministic catalog identity. Prove repeated calculations and ordering are stable; ensure operational values such as configured branch do not affect the public contract; ensure contractual schema/version/authority changes do affect the hash; replace the current one-shot-on-every-start `tools/list_changed` behavior with notification only for a real catalog change supported by the protocol; keep `/version` aligned with the current catalog. Do not begin Hito 4 observability until Hito 3 is committed, tested, reviewed, and recorded.
