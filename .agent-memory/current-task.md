# Current task — MCP redeploy continuity

Authoritative plan: Brain note `mcp-devbox-redeploy-continuity`.

## Hito 0 — completed

- Branch: `fix/mcp-redeploy-continuity`.
- Validated base: `main` / `origin/main` / merge-base `b3d2c160f3179da7966254406587520b735c61ea`.
- Implementation commit: `6fa43f0bd220bc275e162ae91d242b9642068ea1`.
- File added: `internal/mcpserver/redeploy_continuity_test.go`.
- Production baseline: app `jqf7qz5ensoqtvl1tb197gcv` healthy; live commit `b3d2c160f3179da7966254406587520b735c61ea`; 102 tools; catalog `sha256:477bfd598edec2d8c2e03cea3e13c60cc78f898083138e326e8fed55feb8ca1b`.

## Reproduced behavior

The automated same-endpoint replacement fixture proves:

1. Server A accepts `initialize`, `tools/list`, and safe `system_runtime_info`.
2. Server B can start on the same logical endpoint and issue a different new session ID.
3. A new session works after replacement.
4. The old Server A session header is also accepted by Server B.
5. With the same contract, both instances expose the same catalog hash.
6. With a test-only contractual tool added to B, the catalog hash changes.
7. The old session can silently see and call the changed B catalog.

## Confirmed root cause

`internal/mcpserver/http.go` creates one process-wide `Mcp-Session-Id`, returns it on `initialize`, but never validates the client's later `Mcp-Session-Id` header. `tools/list` and `tools/call` do not require a valid initialized HTTP session. Therefore a replacement instance accepts an identifier emitted by the previous instance and grants the current authenticated tool authority through it.

This is a server defect. Separately, ChatGPT may or may not refresh a connector inside an active conversation; that client behavior is not a server guarantee and does not explain the invalid-session acceptance.

## Verification

- `go test ./internal/mcpserver -run TestRedeployContinuityCharacterizesCurrent -count=1` — pass.
- `go test ./internal/mcpserver -count=1` — pass.
- `git diff --check` — pass.
- Diff reviewed; no production implementation changed in Hito 0.
- Tree was clean immediately after implementation commit.

## Decisions and boundaries

- Do not touch the unrelated `h1-edge-runtime-observability` worktree/branch.
- Do not change URL, domain, OAuth configuration, authority modes, jails, grants, Edge, workcells, or add a general shell.
- Preserve persistent Brain and state stores.
- The one-shot SSE catalog notification on every process start is recorded for Hito 3; do not fix it early.

## Exact next step

Hito 1 only: implement bounded graceful draining and readiness. Mark the instance unready before shutdown, reject new MCP initialization during drain, allow active requests to finish within a fixed deadline, close listeners and invalidate in-memory sessions safely, preserve durable state, and keep `/mcp` and `/console` authentication intact. Do not begin Hito 2 session reinitalization semantics until Hito 1 is committed, tested, reviewed, and recorded.
