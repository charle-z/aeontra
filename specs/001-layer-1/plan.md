# Plan — Layer 1

Status: **completed; architecture evolved through P3**.
Governed by `.specify/memory/constitution.md` and `spec.md`.

This is the historical implementation plan for the Layer 1 MVP. It remains useful as
an audit trail, but current architecture and future sequencing are defined by
`docs/context-capsule.md`, `docs/design.md`, `docs/product-roadmap.md`, and the active
`.agent-memory/current-task.md`.

## Original module layout — completed

```text
cmd/mcp-devbox/        # now a strict app.Main composition root
internal/
  app/                 # command/env/runtime/admin/transport composition added in P3
  config/              # immutable startup configuration
  policy/              # jail, secret controls, command policy, grants
  audit/               # append-only redacted audit log
  tools/               # capability services over one shared security core
  mcpserver/           # declarative catalog and stdio/HTTP MCP wiring
```

The original plan described a single `cmd/mcp-devbox/main.go` wiring file and stdio
only. That was correct for the MVP but is no longer the deployed architecture.

## Original build order — completed

1. [x] Minimal secure-default config.
2. [x] Filesystem and command path containment.
3. [x] Secret denial by path.
4. [x] Content scanning and redaction.
5. [x] Command allowlist and destructive/injection blocking.
6. [x] Append-only audit log.
7. [x] One immutable policy decision surface.
8. [x] Read/search/context/write/Git/test/memory tools.
9. [x] MCP stdio server and CLI.
10. [x] Adversarial suite, quality gates, and capsule.

## Preserved design decisions

- Policy remains the single authority for jail, secrets, command posture, redaction,
  approvals, and grants.
- Commands are explicit argv, never shell strings.
- Repository files remain untrusted data.
- Writes remain patch-first and checked before application.
- Risky external operations use exact, expiring, single-use plans with revalidation.
- Public compatibility aliases reuse the same schemas, handlers, and policy path.

## Architecture evolution after Layer 1

- **P0:** deterministic catalog/build identity, no-cache diagnostics, catalog smoke.
- **P1:** declarative catalog modules, aliases, and annotations.
- **P2:** repository, Git, source, platform, and execution capability services over a
  shared policy/audit/root/runner/plan core.
- **P3:** strict executable composition root and focused `internal/app` modules.
- **P4:** targeted Layer-1 hardening; active, behavior-preserving security fixes.

## Verification discipline

Every numbered step uses RED → minimum GREEN → full suite → vet/build → diff review →
documentation/memory → one commit without AI signatures. Phase completion additionally
requires branch audit, baseline, publication/fast-forward, deployment, and production
commit/catalog verification.
