# Spec — Layer 1: Secure local MCP tools (MVP)

Status: **active** · Scope: **L1 only** (see `docs/features.md`).
Governed by `.specify/memory/constitution.md`.

## Goal

A secure-by-default local MCP daemon (Go) that lets an MCP client (ChatGPT, Claude,
Cursor…) read / search / patch / test a local repo **safely**: secrets never leak
(by path AND content), commands cannot leave the jail, every action is audited, and
risky actions ask for approval. Already more secure than Desktop Commander.

## In scope (L1)

- **Policy core** (the product): path jail (fs + commands), secret deny by path,
  content secret-scan + redaction, command allowlist + destructive block, audit log,
  approval gating for risky actions. Policy is immutable at runtime.
- **MCP tools:** `build_context_pack`, `read_file`, `read_many_files`, `search_code`,
  `apply_patch`, `git_status`, `git_diff`, `run_tests`, `memory_read`,
  `memory_update_handoff`. (`project_list`/`project_scan` thin, optional.)
- **Memory:** `.agent-memory/` Markdown + handoffs.
- **Transport / CLI:** stdio MCP transport; `mcp-devbox serve`. (HTTP/tunnel = later.)
- **Tests:** functional per tool + adversarial bypass suite + content-scan redaction.

## Out of scope (NOT this session)

L2 cheap-model worker · L3 OS sandbox / egress control · L4 multi-client install /
Cloudflare Tunnel / relay decision · HTTP transport · UI / dashboard · DB · job queue.

## Actors

- **Orchestrating client** (untrusted in the prompt-injection sense): calls MCP tools.
- **Daemon** (trusted): enforces policy, executes, audits.
- **Repo files** (untrusted DATA): never interpreted as instructions.
- **Human owner**: approves risky actions; owns the policy config (not the agent).

## Functional requirements

| ID | Requirement |
|----|-------------|
| FR-1 | All fs + command operations resolve to a real path **inside** a configured project root; anything else is denied. |
| FR-2 | Reads of secret paths (`.env`, `.env.*`, `.ssh`, key/cred files, browser profiles, OS stores) are denied regardless of jail. |
| FR-3 | Before returning ANY file content, scan for secret patterns and redact matches. |
| FR-4 | Only allowlisted command programs run; destructive commands are blocked even if allowlisted-looking. |
| FR-5 | Writes happen only via `apply_patch`, validated with `git apply --check` before applying; default policy = ask/read-only. |
| FR-6 | Every tool call is appended to an audit log (timestamp, tool, args summary, files, decision, duration). |
| FR-7 | `build_context_pack` returns relevant repo context in one call (fewer roundtrips/tokens). |
| FR-8 | Memory tools read/update Markdown in `.agent-memory/` (jailed, secret-scanned). |
| FR-9 | The policy cannot be modified through any MCP tool at runtime. |

## Security requirements (from `docs/security.md`)

- Path traversal (`../`, absolute, UNC) and symlink escape are blocked.
- Command/arg injection and allowlist bypass (chained `;`/`&&`/`|`, quoting) are blocked.
- A secret cannot be exfiltrated through a permitted command's output (content scan applies to command output too).
- Instructions embedded in repo files are never executed.

## Acceptance (L1 DoD)

`go test ./...` green incl. adversarial suite; `go vet`/build green; tools usable over
stdio MCP; secrets blocked by path+content; commands jailed; audit present; risky
actions gated. `docs/context-capsule.md` updated.
