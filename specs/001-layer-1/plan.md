# Plan — Layer 1

Governed by `.specify/memory/constitution.md` and `spec.md`. Go monolith.

## Module layout

```
cmd/mcp-devbox/         # main: CLI (serve), flag parsing, wiring
internal/
  config/              # project roots + policy config (loaded once, immutable at runtime)
  policy/              # THE CORE — built & tested first, before any tool
    jail.go            # path jail: resolve + contain (fs AND commands)
    secrets.go         # secret deny by path
    scan.go            # content secret-scan + redaction
    commands.go        # command allowlist + destructive block + arg parsing
    policy.go          # Policy struct = composition; single decision surface
  audit/               # append-only audit log
  tools/               # MCP tool implementations (each calls policy first)
  mcpserver/           # MCP protocol wiring over stdio
  memory/              # .agent-memory markdown read/handoff
```

## Build order (strict — core before tools)

1. `config` minimal (project root, policy struct, defaults = secure).
2. **policy/jail** (RED→GREEN) — fs + command path containment.
3. **policy/secrets** — path denylist.
4. **policy/scan** — content secret patterns + redaction.
5. **policy/commands** — allowlist + destructive block + safe arg parsing.
6. **audit** — append-only log.
7. `policy.Policy` composition = the one gate every tool consults.
8. tools (read/search first; apply_patch; git; run_tests; memory).
9. mcpserver stdio wiring + `serve`.
10. adversarial suite consolidation; vet/lint; capsule.

## Key design decisions

- **One decision surface:** `policy.Policy` exposes `CheckRead(path)`,
  `CheckWrite(path)`, `CheckCommand(prog, args)`, `Redact(content)`. Tools never
  re-implement checks; they call these. Easier to audit, no drift.
- **Jail = resolve then contain.** Resolve symlinks (`filepath.EvalSymlinks`) and
  clean the path, then assert it is within an allowed root using a path-segment
  prefix check (not raw string prefix — avoid `/repo-evil` matching `/repo`).
- **Commands are not shell strings.** Reject shell metacharacters; run via exec
  with an explicit argv (no `sh -c`). Allowlist matches the program basename.
- **Content scan applies to ALL returned content** incl. command stdout and patch
  context, so a permitted command can't be used to exfil a secret.
- **Policy immutable:** loaded from config at startup into an unexported struct;
  no setter, no MCP tool can mutate it.
- **Approval gating:** writes/commands carry a risk level; when policy = ask, the
  tool returns an approval-required result rather than executing (client/human gate).

## Dependencies

- Go stdlib first. MCP: use the official Go SDK (`github.com/modelcontextprotocol/go-sdk`)
  if it resolves cleanly; otherwise a minimal JSON-RPC stdio loop (small, no security
  surface). Decide at step 9 after the core is proven. `git` invoked as a subprocess
  (allowlisted, jailed) for `apply_patch`/`git_status`/`git_diff`.

## Verification per step

`go test ./internal/<pkg>/... -run ...` (RED first), then `go test ./...`,
then `go vet ./...` + `go build ./...`. One commit per step, no AI signature.
