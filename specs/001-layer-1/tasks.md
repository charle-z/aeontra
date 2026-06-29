# Tasks — Layer 1

Each task = one TDD step = one commit. RED→GREEN→suite→vet→commit.
Adversarial tests are part of the same step that builds the control.

- [ ] **T01 Setup** — Go toolchain, git+branch, `go mod init`, module layout, AGENTS.md commands.
- [ ] **T02 config** — secure-default `Config`/`Policy` structs; load project root; immutable.
- [ ] **T03 path jail** — `CheckRead/CheckWrite(path)` contain to roots. Adversarial: `../`, absolute, UNC, symlink escape.
- [ ] **T04 secret deny by path** — `.env`, `.env.*`, `.ssh`, key/cred/browser/OS-store paths denied. Adversarial: case, nested, traversal-to-secret.
- [ ] **T05 content scan + redact** — `Redact(content)`: API keys, tokens, `BEGIN ... PRIVATE KEY`, common creds. Adversarial: secret in source/log not just .env.
- [ ] **T06 command allowlist + destructive block** — `CheckCommand(prog,args)`. Adversarial: chained `;`/`&&`/`|`, quoted, arg-injection, non-allowlisted, destructive.
- [ ] **T07 audit log** — append-only JSONL: ts, tool, args summary, files, decision, duration.
- [ ] **T08 policy composition** — `policy.Policy` single gate; verify no runtime mutation path.
- [ ] **T09 read tools** — `read_file`, `read_many_files` (jail+secret+scan); prompt-injection: content returned as data.
- [ ] **T10 search_code** — jailed search, secret paths skipped, matches redacted.
- [ ] **T11 build_context_pack** — one-call relevant context (tree + key files + memory), redacted.
- [ ] **T12 apply_patch** — patch-first; `git apply --check` before apply; jailed; ask-gated.
- [ ] **T13 git_status / git_diff** — controlled git read; diff output redacted.
- [ ] **T14 run_tests** — allowlisted test command only; output redacted; jailed.
- [ ] **T15 memory** — `memory_read`, `memory_update_handoff` over `.agent-memory/` (jail+scan).
- [ ] **T16 mcpserver + serve** — stdio MCP wiring; every handler audits + gates.
- [ ] **T17 adversarial consolidation** — full bypass suite green; `go vet`/build green; capsule update.
