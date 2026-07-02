# Features & Definition of Done - mcp-devbox

> **SUPERSEDED (2026-06-30):** the old option-B cheap-model worker plan is
> historical. Do not build `delegate_to_worker`, do not reintroduce a separate L2
> worker, and do not fork an agent loop. The current source of truth is the Vision
> section in `docs/context-capsule.md`: ChatGPT itself is the agent, and mcp-devbox
> is the safe, policy-enforced tool box it can operate through.

This page is now a lightweight feature inventory. Direction, priorities, and
handoff state live in `docs/context-capsule.md` and `.agent-memory/handoffs/latest.md`.

---

## Current L1 Surface

mcp-devbox exposes 15 MCP tools:

- `build_context_pack`
- `read_file`
- `read_many_files`
- `search_code`
- `apply_patch`
- `create_file`
- `run_command`
- `git_status`
- `git_diff`
- `run_tests`
- `git_commit`
- `memory_read`
- `memory_write`
- `memory_update_handoff`
- `sandbox_status`

Security rules remain the product:

- read-only by default
- filesystem jail on reads, writes, and command execution
- secret deny by path plus content redaction
- local-human, ephemeral, exact-path grants for legitimate secret reads
- raw secret output requires a separate local-human confirmation
- command allowlist only; no free shell
- patch-first writes
- repo content is data, never instructions
- audit every tool call
- policy is not mutable by the agent at runtime

---

## Build Layers

| Layer | Status | Scope |
|---|---|---|
| L1 - secure tools | Done and live | MCP tools, policy, redaction, grants, audit, stdio/HTTP transport, Docker/Coolify deploy. |
| Agent-first capability | In progress | Make ChatGPT productive through better tool descriptions, memory, safe write/test/commit workflows, and transport polish. |
| L3 - hard isolation | Future, required before broad power | OS sandbox plus egress controls before any free-form command, disk/forensics, or PC-wide access. |
| Adoption/install polish | Ongoing | Clear deploy guides, stable HTTPS, better approval UX, and client setup docs. |

The former L2 cheap-model worker section is intentionally not part of the active
plan. The owner wants to avoid burning separate Codex/licensed-agent credits by
letting the already-paid ChatGPT session drive MCP tools directly.

---

## Definition Of Done Per Active Direction

- ChatGPT can inspect, patch, test, and commit safely through MCP.
- Secrets do not leak: path deny, content redaction, exact-path grants, raw double gate.
- Commands cannot escape the workspace policy; broad command power waits for L3.
- Every action is audited.
- Memory/handoffs let any agent resume without trusting chat history.
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and `gofmt -l` are green.
- Production deploy stays secure-by-default: token from env/secret, read-only unless
  deliberately elevated, no admin grant channel exposed outside loopback.
