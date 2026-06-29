# Context Capsule — mcp-devbox

Compact handoff for any AI session. Keep short and current.

## Current Goal

Stand up **Layer 1** (secure-by-default local MCP daemon in Go) — already more
secure than Desktop Commander, dogfoodable daily. **L1 must be finished/shipped
before starting L2** (the cheap-model worker).

## Vision (chosen: option B)

Full target = a **local, secure coding agent powered by a cheap model, orchestrated
by any chat via MCP** (ChatGPT orchestrates; DeepSeek/MiniMax worker does grunt
work; MCP enforces security). Complete feature spec + per-layer DoD in
**`docs/features.md`**. Build L1 → L2 (worker) → L3 (OS sandbox/egress) → L4 (adoption).
The worker-loop spec WILL evolve — do not freeze it.

## Current State

**Layer 1 green + v0.2 remote connectivity + ephemeral human access grants added.** Go module
`github.com/carbe/mcp-devbox` (Go 1.26). Policy core + all 10 L1 MCP tools + MCP
server + CLI, TDD-tested (incl. adversarial), `go vet`/`gofmt` clean. On branch
`feat/layer-1`.

**Transports:** stdio (default, local clients) **and** HTTP (`serve --http ADDR`,
streamable-HTTP subset: `POST /mcp` JSON-RPC, **mandatory bearer auth**, 202 for
notifications, 405 on GET, batch support, `/healthz`). Same Policy/Service/redaction
reused — no core duplication. Bearer from `MCP_DEVBOX_TOKEN` (or `--http-token`);
refuses to start over HTTP without it; binds 127.0.0.1 by default.

**Ephemeral grants:** secret paths remain denied by default. A denied secret read
returns structured `access-required` with a request id. Only the local human can
approve it through the daemon's loopback admin channel using `mcp-devbox grant`;
grants are in-memory, exact-path, single-use, TTL-bounded, and normal grants still
redact. Raw output requires `--raw --confirm-raw`. No grant/approval MCP tool exists.

**VPS/Coolify deploy path:** `Dockerfile` + `.dockerignore` + `docs/deploy-coolify.md`
support running mcp-devbox on a VPS behind Coolify/Traefik with repos mounted at
`/repos`, HTTP bound to `0.0.0.0:8765` inside the container, non-root Alpine
runtime, and `MCP_DEVBOX_TOKEN` supplied only by Coolify env/secrets.

**Verified end-to-end through a real Cloudflare quick tunnel** (2026-06-29):
public `/healthz`→200, `/mcp` no-token→401, `initialize` ok with bearer, and
`read_file` returned the secret **redacted** over the public internet path. The only
step left for the user is the ChatGPT connector click-through (guide below). NOT
built: L2 worker, L3 OS sandbox/egress.

Spec-Kit-style artifacts (constitution + spec/plan/tasks) live in `.specify/` and
`specs/001-layer-1/`. The Spec Kit CLI itself was not installable here (no `uv`), so
we followed the constitution + TDD directly.

### What works (L1)
- **Policy** (`internal/policy`): path jail (fs+commands, symlink/traversal/UNC/
  sibling-prefix safe), secret deny by path, content secret-scan + redaction,
  command allowlist + destructive/injection block, ephemeral in-memory read grants,
  single immutable policy/config gate.
- **Audit** (`internal/audit`): append-only JSONL, secret-scrubbed, concurrency-safe.
- **Tools** (`internal/tools`): build_context_pack, read_file, read_many_files,
  search_code, apply_patch (git apply --check, ask-gated), git_status, git_diff,
  run_tests (allowlist, mode-gated), memory_read, memory_update_handoff.
- **Server/CLI**: `internal/mcpserver` (dependency-free JSON-RPC stdio) + `cmd/mcp-devbox serve`.
- **Deploy:** Docker image for Coolify/VPS documented in `docs/deploy-coolify.md`.

### Toolchain note (Windows host)
Go installed as a local SDK at `C:\Users\carbe\go-sdk\go\bin` (no admin). Prepend it:
`$env:PATH = "C:\Users\carbe\go-sdk\go\bin;" + $env:PATH`.

## Key Decisions

- Go-first core (cross-platform); Rust NOT used (security = architecture, not language).
- Linux-first secure mode via WSL2 on Windows.
- Cloudflare Tunnel + Access (free) for the ChatGPT bridge.
- Markdown repo memory; patch-first writes; allowlist-only commands.
- Positioning: the **secure** alternative to Desktop Commander (core category exists).

## Important Files

| File | Why |
|---|---|
| `AGENTS.md` | operating rules + security invariants (read first) |
| `docs/design.md` | architecture, decisions, MVP scope, layers |
| `docs/security.md` | threat model + secure-by-default invariants + tests |

## Commands

```bash
go test ./...     # full suite (incl. adversarial)
go vet ./...      # quality gate (golangci-lint not installed)
go build ./...    # build
go run ./cmd/mcp-devbox serve --root <ABS_PATH> [--mode read-only|ask|allow] \
       [--test-cmd "go test ./..."]
mcp-devbox grant --admin http://127.0.0.1:<PORT> --admin-token <TOKEN> \
       [--ttl 5m] [--raw --confirm-raw] <REQUEST_ID>

# Remote (HTTP) — bearer auth required:
$env:MCP_DEVBOX_TOKEN = "<token>"      # PowerShell; or export in bash
go run ./cmd/mcp-devbox serve --root <ABS_PATH> --http :8765
cloudflared tunnel --url http://127.0.0.1:8765   # ephemeral public HTTPS URL
# cloudflared 2025.11.1 already installed on this host (via chocolatey).
# Full guide: docs/connect-remote.md (ChatGPT connector + stdio fast-path).
# VPS/Coolify guide: docs/deploy-coolify.md
```

## Open Decisions (resolve before/during build)

- **Install model: self-tunnel (A) vs hosted relay (B)** — security narrative vs
  adoption. Default to (A) for a security tool. See `docs/design.md`.
- **Repo location: WSL2 vs Windows FS** — affects path jail + performance.
- **Validate ChatGPT-web premise** with a real session before heavy build.

## Known Risks / Debt

- Core category exists (Desktop Commander, ~5.7k★, hosts its own relay) — our
  differentiator is **security posture + memory + agent-first tools**, not features.
- ChatGPT↔local needs a tunnel = exposing a local security tool (the hard part).
- Layer 2 (OS isolation) is the genuinely complex piece; **wrap, don't reinvent**.
- Path-based secret blocking is insufficient → need **content secret scanning**.
- The "secure" claim is a liability → under-promise, ship `SECURITY.md`.
- Not yet validated: this is also the first real test of the ai-sdlc-blueprint.

## Next Steps

1. DONE (2026-06-29): ChatGPT web connected via the connector (URL `…/mcp?key=`,
   "Sin autenticación") through a Cloudflare quick tunnel and successfully called
   `build_context_pack` to read+summarize a real repo. The ChatGPT-web premise is
   validated end-to-end. Next: a minimal demo (read → apply_patch → run_tests in
   `ask` mode) to show the full loop.
2. ChatGPT auth: RESOLVED. ChatGPT's UI has no header field (only OAuth/none/mixed), so
   the token rides in the URL (`?key=`). Upgrade path (no URL secret): OAuth via
   Cloudflare Access once a domain is set up.
3. For a stable URL, set up a **named** Cloudflare tunnel (guide Step 3b).
4. Merge `feat/layer-1`.
5. Only then start **L2** (cheap-model worker) per `docs/features.md`. Do NOT begin
   L2 until connectivity is dogfooded and the branch merged.

## Last Verified

Date: 2026-06-29 — `go test ./...` + `go vet` + `go build ./...` + `gofmt` green. stdio: initialize/
tools/list/tools/call, secret read returns structured `access-required`,
human-approved grants are exact-path/single-use/TTL and raw-gated, prompt-injection
returned as data.
HTTP: 401 without bearer, unauthenticated `GET /mcp` -> 405, redaction over transport, 202/405/413/batch. **Remote:
verified through a live `trycloudflare.com` quick tunnel — public 401 without token,
`initialize` ok with bearer, `read_file` redacted over the public path; tunnel torn
down after.** Docker image smoke passed on Docker Desktop 4.55.0: image built,
container healthy, `/healthz` -> 200, `GET /mcp` -> 405, `POST /mcp` without token
-> 401, `POST /mcp?key=` initialize -> 200. ChatGPT UI click-through pending
(user step).
