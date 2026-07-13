# ADR 0001: deterministic catalog, product foundations, and delivery order

- Status: Accepted
- Date: 2026-07-12
- Scope: `mcp-devbox`, validation runner, console, CI/CD
- Amendment: P8 console item 6 and the Astro/BFF choice are superseded by ADR 0002.

## Context

The deployed MCP can change while a connected client continues showing an older
`tools/list` surface. The codebase also has version/tool-count drift across runtime,
tests, and documentation. Meanwhile, `internal/mcpserver/tools.go`, the central
`tools.Service`, and `cmd/mcp-devbox/main.go` are approaching sizes where adding the
operator console, observability, and an asset broker would increase coupling.

The product must remain resumable by another agent without depending on chat
history. Existing environment-variable names and public tool names are deployed
contracts and must not be broken during the refactor.

## Decision

Work proceeds in this order:

1. P0: deterministic tool catalog, build identity, safe diagnostics, cache evidence.
2. P1: split MCP tool registration by domain without changing the wire surface.
3. P2: split the tool service by capability while keeping policy/audit/plans central.
4. P3: reduce `cmd/mcp-devbox/main.go` to a composition root without renaming envs.
5. P4-P7: L1 hardening, testing, CI/DevSecOps, and observability.
6. P8: operator console; implementation choice superseded by ADR 0002.
7. P9: an asset broker with provenance, licensing, ranking, and automatic selection.

Every numbered implementation step follows RED -> GREEN -> REFACTOR -> full suite ->
quality gates -> one commit. Runtime behavior may only change when the step's design
and tests explicitly require it.

## Compatibility invariants

- Existing environment-variable names keep their meaning.
- Existing MCP tool names, aliases, schemas, handlers, annotations, and approval
  posture remain compatible during P0-P3.
- The central policy remains the sole authorization authority.
- Secret values never enter tool manifests, action plans, logs, metrics, docs, or
  frontend bundles.
- The public MCP container never receives a Docker socket.
- The validation runner remains private and fixed-profile; it is not a free terminal.

## P0 catalog contract

A deterministic catalog will be the source for:

- `tools/list`;
- tool count;
- SHA-256 catalog hash;
- annotations and schemas;
- generated documentation checks;
- safe runtime build diagnostics.

The hash must be stable across process runs and map iteration order. It covers tool
name, tool version, input schema, and annotations. Handler pointers and descriptions
are not security identities and are not directly hashed unless the contract later
requires it.

The runtime will expose only non-sensitive build information:

- semantic version;
- commit;
- build time when injected;
- protocol version;
- tool count;
- catalog hash.

HTTP responses for MCP and diagnostics must use no-store/no-cache headers so reverse
proxies and browsers cannot be confused with the client's own connector catalog
cache. A post-deploy smoke check compares expected commit/hash/count with the live
runtime. Client-side staleness is documented only after server-side identity is
verified.

## Logs and retention

Logs are structured, redacted, and bounded. Retention is configuration, not a hard
coded product limitation.

Default production policy:

- application/audit logs rotate daily or at a configured size threshold;
- retain 14 daily files locally;
- compress rotated files;
- cap total retained bytes;
- delete expired files automatically;
- never rotate away the currently open file;
- deployment/build artifacts use shorter retention unless marked for an incident;
- metrics aggregate long-term behavior instead of retaining unlimited text logs.

Large log reads use pagination/cursors and bounded responses. Jobs retain status and
summary independently of full text output.

## Container policy

Images use multi-stage builds. The final image contains only the runtime binary,
required CA certificates/runtime assets, and explicitly justified developer tools.
Dependencies are copied at the narrowest useful granularity. Build caches and SDKs
must not leak into runtime layers.

Exceptions are documented per image. The validation runner may require the Docker
CLI/socket and mounted workspaces, but remains isolated from the public MCP.

## Ephemeral staging and DAST

DAST runs against an isolated staging deployment with synthetic data and no
production credentials. The environment has a TTL and is destroyed after the test
or by a cleanup job after failure. Passive/baseline scanning is the initial gate;
active scanning requires an isolated target and a separate approval.

## Console security

The original Astro SSR + server-side BFF implementation choice is superseded by
ADR 0002. The enduring controls remain: contextual output safety, no untrusted raw
HTML, strict CSP, secure/SameSite cookies, server-side validation, authentication on
every private route, no browser-accessible infrastructure token, and adversarial
security testing.

## Skills and agent continuity

Skills are reviewed accelerators, not authority. Each used skill is recorded in
`docs/skills-registry.md` with source, version/commit, purpose, and review outcome.
Repository docs, tests, commits, and deployed identity remain the source of truth.

Every completed step updates the context capsule or a handoff with objective, changed
files, verification, commit, rollback, risks, and the next exact step.

## Consequences

This approach adds a small amount of design and generated metadata work before visible
console features. In return, it makes deployments observable, refactors reviewable,
security contracts testable, and work transferable between agents without relying on
conversation history.
