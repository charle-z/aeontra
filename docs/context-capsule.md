# Context Capsule — mcp-devbox

Compact handoff for any AI session. Keep short and current.

## Current Goal

L1 + remote connectivity are **done and running in production** (ChatGPT web →
Coolify/VPS over HTTPS, verified live). Current goal: evolve it into a **GPT-driven
agent** that can safely *do* things (not just read), controlled by the human via the
ChatGPT chat.

## Vision (UPDATED 2026-06-30 — supersedes the old "option B" worker plan)

**Owner's refined direction (confirmed in chat):** the agent IS ChatGPT itself.
ChatGPT (the chat the owner already pays for) drives the loop directly via MCP tools;
mcp-devbox is the **safe hands**. Goals: control the VPS now, optionally the PC later,
**without burning Codex / licensed-AI credits**, non-persistent and human-controlled.

> **DROPPED: the L2 cheap-model worker (DeepSeek/MiniMax).** The owner explicitly does
> NOT want a separate worker/orchestrator — that was "the thing I'd send orders to";
> they want GPT itself to act. Do NOT reintroduce a worker. Do NOT fork opencode/etc.
> (those bring their own paid model loop). ChatGPT already does multi-step tool-calling.

**Revised layers / priorities:**
1. **More agent-first tools + capability** — flip `ask`, wire `MCP_DEVBOX_TEST_CMD` /
   `MCP_DEVBOX_ALLOW_CMD` (env→flags), add write/create tools so GPT *does*, not just reads.
2. **Grants hardening** — the deliberate, human-approved bypass of secret-deny must stay airtight.
3. **L3 (OS sandbox + egress)** — REQUIRED before free command execution, especially on the PC.
4. **Easy install / portability** — one-command redeploy to any VPS (already Dockerfile + Coolify).

`docs/features.md` still describes the old option-B worker; treat THIS section as the
current source of truth for direction.

**Image policy (decided 2026-06-30):** do NOT minimize the runtime image to bare
essentials. The owner expects the box to GROW into a broader agent (run tests now;
later possibly disk access / forensics / more toolchains) — "a Codex-in-chat with
OpenClaw/Hermes-like freedom". So the runtime keeps a capable toolset (currently Go
1.26 + git). Trading capability for a few hundred MB is not worth it on this VPS.

**Security consequence (non-negotiable as capability grows):** every new capability
is a deliberate, allowlisted, audited tool + an explicit jail expansion — never a
blanket "free terminal". And broad capabilities (disk/forensics/network) make **L3
(OS sandbox + egress default-deny) a hard prerequisite**, not optional: a command
with disk+network access reachable from ChatGPT is a serious hole without L3. Evaluate
that expansion carefully; do not bolt it on before L3.

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

**Secret-scan tuning (2026-06-30):** content redaction still catches provider
tokens and real generic assignments, but no longer redacts obvious non-secret
assignment values such as shell command substitutions (`$(...)`), env-var refs
(`$TOKEN`, `${TOKEN}`, `$env:TOKEN`), and placeholders (`<paste-the-token>`,
`REPLACE_ME...`, `your-token-here`).

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

## Deployment in production (2026-06-30)

Running on a CubePath VPS via **Coolify** (Dockerfile build), public at
`https://mcp-devbox-charlez.duckdns.org` (DuckDNS A → 144.225.147.58, Traefik TLS via
Let's Encrypt). ChatGPT connector: URL `…/mcp?key=<MCP_DEVBOX_TOKEN>`, auth "Sin
autenticación". Verified live from ChatGPT: build_context_pack / search_code / read_file
work; `.env` correctly blocked with `access-required`. Repo to operate on is cloned into
the `/repos` volume; `MCP_DEVBOX_ROOT=/repos/mcp-devbox`, `MCP_DEVBOX_MODE=read-only`.
Gotcha solved: Coolify "Ports Exposes" must be **8765** (was 3000 → 502).

### Approving a secret-read grant on the VPS
Secret reads return `access-required` + a `request_id`. To approve (human-only, by design):
1. Coolify → app → **Logs**: find the `ACCESS REQUIRED … mcp-devbox grant --admin
   http://127.0.0.1:<port> --admin-token <tok> --ttl 5m <request_id>` line.
2. Coolify → app → **Terminal** (inside the container), run that exact command.
   The admin channel is loopback-only, so it must be run from inside the container.
   `--raw` (unredacted) additionally requires `--confirm-raw`.

## Next Steps (per the UPDATED vision above — GPT-as-agent, NO worker)

1. **Capability:** wire `MCP_DEVBOX_TEST_CMD` + `MCP_DEVBOX_ALLOW_CMD` (env→flags in
   `cmd/mcp-devbox/main.go`, mirror in Dockerfile CMD), flip VPS to `--mode ask`, so GPT
   can patch + run tests. Then add write/create tools (new files, controlled git commit).
2. **Grants hardening/verify:** confirm grants are single-use + path-exact + non-persistent
   + raw double-gated (code looks correct; add/confirm adversarial tests). Consider a less
   painful approval UX than container-exec.
3. **L3:** OS sandbox (wrap Docker/gVisor/nsjail) + egress default-deny — required before
   free command exec, especially for the PC scenario.
4. **PC scenario (optional):** daemon on PC bound to 127.0.0.1 + `ssh -R` to the VPS reusing
   the domain; keep read-only/ask until L3.
5. **Install polish:** one-command redeploy to a new VPS.

NOTE: `feat/layer-1` history was squashed; everything now lives on `main` (and GitHub
`charle-z/mcp-devbox`). `docs/features.md` still mentions the L2 worker — outdated; the
Vision section above wins.

## Last Verified

Date: 2026-06-30 — `go test ./... -count=1` + `go vet ./...` + `go build ./...` + `gofmt` green. Secret-scan false positives tuned for command substitutions/env refs/placeholders while real assignments still redact. stdio: initialize/
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
