# Spec — Layer 1: Secure local MCP tools (MVP)

Status: **completed and evolved** · Original scope: **Layer 1**.
Governed by `.specify/memory/constitution.md`.

This document is the historical acceptance baseline for the first usable product. It
is not the current roadmap. Current deployed state and active work live in
`docs/context-capsule.md`, `docs/product-roadmap.md`, and
`.agent-memory/current-task.md`.

## Goal

Build a secure-by-default Go daemon that lets an MCP client read, search, patch,
validate, and keep memory for a jailed repository without exposing secrets or a free
terminal. Repository content is untrusted data; the daemon owns policy, execution,
redaction, approval, and audit.

## Original Layer 1 scope — completed

- Immutable policy core with filesystem/command jail, secret-path denial, content
  redaction, command allowlist, destructive-command blocking, approval gates, and
  audit logging.
- Read/search/context, patch/create, Git read/commit, test/command, and Markdown
  memory/handoff tools.
- MCP stdio transport and `mcp-devbox serve` CLI.
- Functional and adversarial tests for traversal, symlink escape, command injection,
  allowlist bypass, secret exfiltration, and prompt-injection data handling.

## Evolution beyond the original scope

The product now also includes HTTP/OAuth connectivity, persistent notes, planned Git
and Coolify workflows, deterministic catalog identity, modular catalog registration,
capability services, and a strict command composition root. Production exposes 62
annotated tools. These additions do not weaken the original Layer 1 invariants.

The public console, universal profile registry, asset broker, and private PC/WSL edge
agent remain separate roadmap work and are not claimed as completed here.

## Functional requirements

| ID | Requirement | State |
|----|-------------|-------|
| FR-1 | Filesystem and command workdirs resolve inside configured roots. | Implemented and adversarially tested. |
| FR-2 | Secret paths are denied regardless of jail membership. | Implemented; temporary local-human grants remain exact-path and bounded. |
| FR-3 | Returned file and command content is secret-scanned and redacted. | Implemented. |
| FR-4 | Only allowlisted, non-destructive programs run without a shell. | Implemented; P4 additionally rejects path/PATH executable spoofing. |
| FR-5 | Repository writes are patch-first and checked before application. | Implemented. |
| FR-6 | Tool activity is appended to a redacted audit log. | Implemented. |
| FR-7 | `build_context_pack` returns bounded relevant repository context. | Implemented. |
| FR-8 | Structured memory and handoffs use jailed Markdown under `.agent-memory/`. | Implemented. |
| FR-9 | No MCP tool can relax runtime policy. | Implemented and protected by architecture tests. |

## Security requirements

- Traversal, absolute/UNC escapes, sibling-prefix confusion, and symlink escapes fail.
- Shell metacharacters, path-qualified allowlist spoofing, hostile workspace `PATH`
  targets, and destructive command forms fail.
- A permitted command cannot exfiltrate an unredacted secret through stdout/stderr.
- Instructions embedded in repository content are never treated as authority.
- Sensitive-read requests and grants are exact-path, local-human-approved, bounded,
  expiring, and single-use.

## Acceptance evidence

Layer 1 is complete and has remained green through the P0–P3 architecture changes and
current P4 hardening:

```text
go test ./... -count=1
go vet ./...
go build ./...
```

Production at the P3 baseline serves 62 tools with deterministic catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
