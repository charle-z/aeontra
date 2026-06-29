# Features & Definition of Done — mcp-devbox (full vision: option B)

> North star: a **local, secure coding agent powered by a cheap model, orchestrated
> by any chat (ChatGPT) via MCP.** The chat orchestrates at high level; a cheap
> local model (DeepSeek/MiniMax) does the agentic grunt work; the MCP enforces
> security. This is the COMPLETE target. **Build in layers (below). The spec will
> evolve — especially the worker loop. Don't freeze it; don't build it all at once.**

---

## Feature inventory (the complete "what")

### A. MCP tools (interface the orchestrating chat calls)
- `project_list`, `project_scan`, `build_context_pack`
- `read_file`, `read_many_files`, `search_code`
- `apply_patch`, `git_status`, `git_diff`
- `run_tests` (allowlist)
- `memory_read`, `memory_update_handoff`
- **Compound (agent-first):** `prepare_change(task)`, `implement_change(task)`
- **Delegation (B):** `delegate_to_worker(task, model)`

### B. Security / policy (the differentiator)
- read-only default; write/commands = ask; per-action approval for risky ops
- filesystem jail that **also covers command execution**
- secret deny by **path** (`.env`/`.ssh`/keys) **+ content-level secret scanning/redaction**
- command allowlist; destructive-command block
- patch-first writes (validate `git apply --check` before applying)
- repo file content treated as **data, never instructions** (prompt-injection defense)
- per-project policy config (allowed paths/commands)
- **policy NOT mutable by the agent at runtime**
- audit log (who/what/when/files/duration)

### C. Worker / agent loop (B — the hard, ambitious layer)
- pluggable cheap-model backend (DeepSeek/MiniMax via OpenAI-compatible client)
- agentic loop: plan → act → observe → iterate, with goal + termination
- context management (fed by `build_context_pack`; compaction/summarization)
- test-driven self-correction (run tests → fix on failure → repeat)
- error recovery / retries
- **step + cost budget** (cap iterations and tokens; never run forever)
- worker runs **under the same security policy** (jail, allowlist, secret deny)
- human approval gates for risky actions even inside the loop

### D. Memory (agent-agnostic, lives in the repo)
- `.agent-memory/` (project-state, current-task, decisions, conventions, commands,
  known-issues, handoffs/latest)
- `memory_read` / `memory_update_handoff`
- rule: every agent reads memory in, updates handoff out
- generators: `AGENTS.md` / `CLAUDE.md` / cursor rules that **point to** `.agent-memory`

### E. Isolation (wrap, don't reinvent — Linux/WSL2)
- OS sandbox wrap (gVisor / nsjail / Docker) so commands can't escape the policy
- egress control: default-deny outbound; block 169.254.169.254 + RFC1918; allowlist endpoints

### F. Connectivity / install
- CLI: `init`, `add-project`, `list-projects`, `doctor`, `serve`, `audit`, `policy check`
- MCP transport: streamable HTTP (+ stdio for local clients)
- tunnel: **self-hosted Cloudflare Tunnel** (secure default; relay = pending decision)
- auth: bearer/OAuth on the daemon
- per-client setup guides (ChatGPT, Claude, Cursor, opencode)

### G. Ops / trust
- `SECURITY.md` + vulnerability disclosure policy
- policy lint (warn on dangerous allowlist entries / allowlist drift)
- optional usage stats

### H. Testing (security-first)
- **adversarial bypass tests**: path traversal, symlink escape, arg-injection,
  allowlist bypass, secret exfil via permitted command, prompt-injection from a file
- functional tests per tool
- worker-loop tests (mock model)
- content secret-scan redaction tests

---

## Build layers (how the "all" gets built without building all at once)

| Layer | Scope | DoD — "I arrived at this layer when…" |
|---|---|---|
| **L1 — Secure tools (MVP)** | A (basic tools + compound) + B-security + D-memory + F-CLI/transport + H-security tests | ChatGPT (via MCP) can read/search/patch/test a local repo **safely**: secrets never leak (path+content), commands can't leave the jail, every action audited, risky actions ask. Adversarial bypass tests pass. Already more secure than Desktop Commander. |
| **L2 — Cheap-model worker (B)** | C (worker loop) + `delegate_to_worker` | ChatGPT orchestrates a real task → the cheap-model worker implements it agentically (edit→test→self-correct) under policy, within step/cost budget, with approval gates. Codex-like autonomy without Codex credits. |
| **L3 — Hardening** | E (OS sandbox + egress) + G (SECURITY.md, policy lint) | A permitted command **provably cannot escape** (OS-sandboxed) and **cannot exfiltrate** (egress default-deny). Disclosure policy published. |
| **L4 — Adoption** | multi-client generators, install polish, tunnel/relay decision | Any MCP client connects via guided setup; install is "as easy as a local secure tool can be". |

---

## Full Definition of Done (the complete vision — "I built what I needed")

All of the following true at once:
- [ ] ChatGPT (Plus) orchestrates a real coding task on a local repo end to end.
- [ ] A cheap local model (DeepSeek/MiniMax) executes it agentically: edit → run tests → self-correct, within a step/cost budget.
- [ ] Secrets never leak — blocked by path AND redacted by content scan.
- [ ] Commands cannot escape the workspace (OS-sandboxed) and cannot exfiltrate (egress default-deny).
- [ ] Every action audited; risky actions require human approval.
- [ ] Memory persists across sessions and agents; handoffs let any agent resume.
- [ ] All adversarial bypass tests pass; full suite + lint/type green.
- [ ] Installs via guided CLI + Cloudflare Tunnel; `SECURITY.md` published.
- [ ] Dogfooded: the owner uses it daily instead of burning Codex credits.

> Realistic note: you will hit DoD **per layer**, in order. Do not wait for the full
> DoD to ship/use — L1 alone is usable and beats Desktop Commander. "Hard" only
> counts if **finished**; finish L1 before starting L2.
