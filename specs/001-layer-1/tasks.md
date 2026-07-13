# Tasks — Layer 1

Status: **completed**. Each original task was delivered with tests and remains covered
by the current full suite. New architecture/security work is tracked by numbered P0+
steps in `.agent-memory/current-task.md` and dated baselines under `docs/baselines/`.

- [x] **T01 Setup** — Go module, repository layout, agent instructions, build commands.
- [x] **T02 config** — secure-default immutable startup configuration.
- [x] **T03 path jail** — filesystem/command containment with traversal, UNC, sibling-prefix, and symlink defenses.
- [x] **T04 secret deny by path** — `.env*`, `.ssh`, keys, credentials, browser/OS stores.
- [x] **T05 content scan + redact** — provider tokens, private keys, credential assignments, output redaction.
- [x] **T06 command allowlist + destructive block** — explicit argv, no shell, injection/destructive blocking.
- [x] **T07 audit log** — append-only, redacted, concurrency-safe JSONL.
- [x] **T08 policy composition** — one immutable policy authority with no MCP mutation path.
- [x] **T09 read tools** — jailed, secret-aware, redacted single/multi-file reads.
- [x] **T10 search_code** — jailed search, secret-path skipping, redacted matches.
- [x] **T11 build_context_pack** — bounded repository context, memory, tree, and Git state.
- [x] **T12 apply_patch** — patch-first writes with `git apply --check` and approval gates.
- [x] **T13 git_status / git_diff** — controlled Git reads with redacted output.
- [x] **T14 run_tests** — configured allowlisted command, jailed cwd, redacted output.
- [x] **T15 memory** — structured Markdown memory and latest handoff support.
- [x] **T16 mcpserver + serve** — stdio MCP wiring, centralized handlers, audit and gates.
- [x] **T17 adversarial consolidation** — bypass suite, full tests, vet/build, capsule.

## Follow-on phases

- [x] P0 deterministic catalog and deployment identity.
- [x] P1 catalog modularization.
- [x] P2 capability-service split.
- [x] P3 composition root.
- [ ] P4 targeted Layer-1 hardening — active on `p4-l1-hardening`.
- [ ] P5 deeper testing.
- [ ] P6 CI/DevSecOps gates.
- [ ] P7 structured observability.
- [ ] Authenticated console, asset broker, universal profiles, and edge-agent work as
  separate roadmap milestones with their own specs and acceptance evidence.
