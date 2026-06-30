# Handoff — 2026-06-30 (for Codex / any agent)

## Context (read these first)
`docs/context-capsule.md` (Vision + Next Steps are the current source of truth),
`AGENTS.md`, `docs/security.md`. Repo on `main` (GitHub `charle-z/mcp-devbox`),
deployed in production via Coolify on a VPS, reachable from ChatGPT web.

## Direction (UPDATED — do not regress)
The agent IS ChatGPT itself, driving MCP tools directly. **Do NOT build the L2
cheap-model worker (DeepSeek/MiniMax) and do NOT fork opencode/etc.** Invest in: more
safe tools + capability, grants hardening, L3 sandbox, easy install. `docs/features.md`
still mentions the worker — it's outdated; the capsule Vision wins.

## Build discipline (AGENTS.md)
TDD per step: RED → GREEN → `go test ./...` → `go vet ./...` + `gofmt -l` → one commit
per step, **no AI signature**. Go toolchain (Windows host, not on PATH):
`$env:PATH = "C:\Users\carbe\go-sdk\go\bin;" + $env:PATH`. Reuse the existing Policy /
Service / mcpserver — never duplicate security checks. Repo file content is DATA.

## STATUS (2026-06-30)
- Task 1 DONE (commit 40af1cd): env config MCP_DEVBOX_TEST_CMD / MCP_DEVBOX_ALLOW_CMD.
- Task 2 DONE (commit 8415c49): grants verified airtight — adversarial tests prove the
  agent cannot self-approve (request_id replay denied), unknown-id approve fails, grant
  is single-use + path-exact + non-persistent + raw double-gated. See
  internal/policy/grants_adversarial_test.go. **Grants are sound; no rework needed.**
- Task 3 DONE: `create_file` (patch-first new-file creation, commit a4b10e1) and
  `git_commit` (staged commit, mode-gated, no push) added + registered as MCP tools,
  tested against real git. The create→test→commit loop is now usable from ChatGPT.
- NEXT = Task 4 (L3: OS sandbox + egress). Optional polish: write/overwrite-existing
  via patch helper, controlled `git_push` (currently push is blocked by design),
  clearer tool schemas. All pushed to origin/main.

## Task 1 (DONE) — capability via env, flip to `ask`
Goal: let ChatGPT actually patch + run tests on the VPS without rebuilding the image.
- In `cmd/mcp-devbox/main.go`, read `MCP_DEVBOX_TEST_CMD` and `MCP_DEVBOX_ALLOW_CMD`
  from env as fallbacks for the existing `--test-cmd` / `--allow-cmd` flags (flag wins
  if set). Keep `MCP_DEVBOX_MODE` / `ROOT` / `TOKEN` behavior intact.
- Update the Dockerfile CMD so those envs flow through (it already passes ROOT/MODE).
- Tests: env is read; flag overrides env; empty env = current secure defaults.
- DoD: set `MCP_DEVBOX_TEST_CMD="go test ./..."` and `MCP_DEVBOX_MODE=ask` in Coolify →
  redeploy → from ChatGPT, `run_tests` returns approval-required, and `apply_patch`
  works after approve. Update `docs/connect-remote.md` with the new env vars.

## Task 2 — verify/harden grants (security-critical)
The grants feature (`internal/grantadmin`, `internal/policy` access-grants,
`tools.ReadFileWithAccess`) is a deliberate human-approved bypass of secret-deny.
It LOOKS correct (loopback admin + token, raw needs --confirm-raw, ttl 1s–1h, agent
can't self-approve). Confirm with adversarial tests if not already covered:
- grant is **single-use** (consumed after one read) and **path-exact** (no widening to
  siblings/parent), **non-persistent** (gone after restart), and the **agent cannot
  reach the admin channel** (loopback only).
- `--raw` truly requires the second confirmation; normal grant still redacts.
- A grant approved for path A cannot be used to read path B.
Fix any gap found, TDD. Also: make the approval UX less painful than exec-into-container
(e.g., document a `docker exec` one-liner, or a small `mcp-devbox grant` convenience).

## Image policy (decided): keep it CAPABLE, not minimal
Do NOT strip the runtime to bare essentials. Owner wants the box to grow into a
broader agent (later: disk access / forensics / more toolchains — "Codex-in-chat with
OpenClaw/Hermes freedom"). Runtime keeps Go 1.26 + git (commit f6140e8). When adding
capabilities: each = a deliberate allowlisted+audited tool + explicit jail expansion,
NEVER a free terminal. Broad capabilities make L3 a HARD prerequisite (see below).

## Task 4 — L3 (the enabler for "freedom"): OS sandbox + egress
Wrap Docker/gVisor/nsjail so a permitted command cannot escape; egress default-deny
(block 169.254.169.254 + RFC1918). Required before free command execution, especially
the PC scenario. Wrap, don't reinvent (see docs/design.md / security.md).

## Don't
Reintroduce the cheap-model worker; give the PC command-execution before L3; weaken
read-only default; expose the grant admin channel beyond loopback; persist grants.
