# Design — mcp-devbox

## Problem

Codex/agent credits burn fast. Goal: use ChatGPT (and other MCP clients) as a
local dev agent over your own repos, **safely**, without paying per-credit and
without giving the agent free reign over the machine.

## What already exists (verified, not assumed)

- **Desktop Commander MCP** (~5.7k★): local dev MCP, supports ChatGPT via remote
  MCP — but **permissive by default** (full filesystem + terminal; no secret
  blocking; no read-only; terminal escapes allowed dirs).
- **Enterprise MCP gateways** (Cerbos, liteLLM, mcpmanager): policy layers, not a
  local dev daemon for individuals.
- **Cloud/container sandboxes** (gVisor, Firecracker, nsjail, Modal): isolate
  execution; not a local-first individual dev tool.

**Gap (narrow but real):** a local-first, secure-by-default dev MCP daemon for
individuals, with agent-agnostic repo memory. No exact match found.

> "agent-agnostic / works for all providers" is **not** a differentiator — that is
> inherent to MCP (one server works with any MCP client). The differentiator is
> **secure-by-default** + **repo memory**.

## Architecture

```text
ChatGPT / Claude / Cursor / opencode  (any MCP client)
  ↓  (Cloudflare Tunnel + Access: TLS, auth, outbound-only)
mcp-devbox daemon (Go)
  ↓  policy gate (read-only default, allowlist, secret deny, path jail, audit)
tools: build_context_pack · list_dir · read_file · read_many_files · search_code ·
       apply_patch · run_command · git_status · git_diff · run_tests ·
       memory_read · memory_update_handoff · sandbox_status
  ↓
local repositories (only configured paths)
```

The current secure-builder surface is larger than this original MVP diagram. The
canonical 55-tool registry, aliases and annotations are in `tools.md`.
Consequential multi-step operations share one in-memory action-plan store:

```text
read-only preview -> cryptographic plan (exact non-secret args + state + TTL)
                  -> mode/approval -> single consume -> state revalidation
                  -> narrow generated effect -> redacted result + audit
```

Plans never mutate policy, never contain secret values, and disappear on restart.

Principle: **the model thinks, the daemon executes, git records, tests validate,
memory persists, the human approves risky actions.**

## Decisions

- **Language: Go-first.** Cross-platform single binary, great for fs/git/process/HTTP.
  The MVP bottleneck is MCP roundtrips, not CPU — so optimize `build_context_pack`
  (one call returns relevant context) over many small `read_file` calls.
  Rust only later for a low-level module if measured need; **not for "security"**
  (security is architecture, not language; Go is memory-safe).
- **Platform: Linux-first via WSL2 on Windows.** Layer-1 policy is cross-platform;
  hard isolation (Layer 2) is Linux-only → run secure mode in WSL2.
- **Tunnel: Cloudflare Tunnel + Cloudflare Access (free tier).** Outbound-only,
  TLS, stable URL, auth in front. No open ports.
- **Memory: Markdown in the target repo** (`.agent-memory/` + `AGENTS.md`),
  never in the chat or in a specific vendor.

## MVP tools (Layer 1)

`build_context_pack · list_dir · read_file · read_many_files · search_code · apply_patch ·
create_file · run_command · git_status · git_diff · run_tests · git_commit ·
memory_read · memory_write · memory_update_handoff · sandbox_status`

Most important: `build_context_pack`, `read_many_files`, `apply_patch` (minimize
roundtrips, minimize tokens, reviewable changes).

## Layered build (proportional — do NOT build all at once)

- **Layer 1 (MVP, Go, app-level policy):** read-only default, secret deny,
  command allowlist, path jail (covering terminal), patch-first, audit log.
  → Already more secure than Desktop Commander. Dogfood it daily.
- **Layer 2 (v0.3, OS isolation):** wrap gVisor/nsjail/Docker so a permitted
  command **cannot escape** the policy. Linux/WSL2. **Wrap, don't reinvent.**
- **Layer 3 (v0.3, egress control):** default-deny outbound; block metadata
  endpoint (169.254.169.254) and RFC1918; allowlist only needed endpoints.
- **Tunnel hardening:** Cloudflare Tunnel + Access + bearer/OAuth on the daemon.

## What NOT to build (yet)

Free terminal · full-file write tool · autonomous agent without approval · DB ·
job queue · UI/dashboard · hosted relay · OpenCode delegation · reinvented sandbox
primitives. (See `AGENTS.md` architecture rule.)

The disabled-by-default privileged profile mechanism is not a free terminal: every
command and parameter shape is defined server-side, has a short plan TTL and timeout,
and Docker profiles fail securely rather than exposing the host Docker socket to the
public daemon.

## Phases

`v0.1 MVP (Layer 1) → v0.2 ChatGPT setup (Cloudflare Tunnel guide) →
v0.3 security (Layers 2-3, tests) → v0.4 multi-agent config gen → v0.5 production`

## Why this exists (premise)

- **ChatGPT-web-first:** Plus web limits reset in hours vs Codex's weekly cap →
  when Codex is exhausted you're blocked a week; with web you continue after hours.
- **Agent-first tools:** compound tools (`build_context_pack`) minimize MCP
  roundtrips → better for cheap/fast models driven from a chat UI, and **less
  consumption of the chat's message limit.** mcp-devbox is the **tools**; the agent
  loop is the **client** (by design — "model thinks, daemon executes").
- **Multi-provider is free, but bounded:** any MCP-capable client works (MCP is
  agnostic), but the chat must actually expose MCP connectors — ChatGPT/Claude yes;
  consumer Gemini/DeepSeek chat = uneven, verify per provider.

## Competitive strategy (vs Desktop Commander)

Out-**secure** the core; do NOT out-**feature** the breadth. Reimplement ideas in
Go; never copy code (DC is open source — learn, don't lift).

| DC strength | Our move |
|---|---|
| Surgical edit (`edit_block`) | Adopt → `apply_patch` validated (`git apply --check`) |
| Multi-project | Adopt |
| Search | Adopt, path-jailed |
| Process control | Adopt but **allowlist-only** (their strength = their hole) |
| Reads full files | Differ → `build_context_pack` (relevant, fewer tokens) |
| Audit/analytics | Adopt (audit log) |
| `set_config` at runtime | **NOT** for security policy (would be a hole) |
| Excel/PDF/DOCX, rich previews | **Skip** — scope creep, dilutes the security wedge |

DC's biggest strength (full power, permissive-by-default) **is** its biggest
weakness. We win on **security posture, not feature count.**

## Install fork — DECISION PENDING (decide before v0.2)

DC made ChatGPT easy by **hosting a relay** (`mcp.desktopcommander.app`): a light
device on your PC connects out to their cloud relay. Two paths for us:

- **(A) Self-tunnel** (user runs Cloudflare Tunnel): more friction, **zero trust in
  us**, fits the security narrative. Honest default for a security tool.
- **(B) We host a relay:** easy install, but we become **the intermediary to
  everyone's PC** = high-value target + liability.

For a security-first tool, **(A)** is the honest default; **(B)** trades the
security narrative for adoption. Not decided.

## Open considerations (contemplate before/during build)

1. **Content-level secret scanning** (not just path-blocking) — see `security.md`.
2. **Adversarial self-testing** (red-team the tool) — see `security.md`.
3. **The "secure" claim is a liability** — under-promise; ship `SECURITY.md`.
4. **Validate the ChatGPT-web premise** with a real session before heavy build.
5. **Repo location: WSL2 vs Windows FS** — affects path jail + performance (owner
   is Windows-first; secure mode is Linux/WSL2). Decide where repos live.
