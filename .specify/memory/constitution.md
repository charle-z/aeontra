# mcp-devbox — Project Constitution

> Source of truth for how the project is built. Derived from `AGENTS.md` and the
> security invariants. If a spec, plan, task, roadmap item, or agent instruction
> conflicts with this constitution, the constitution wins.

Last amended: 2026-07-13.

## Article I — Security is the product (NON-NEGOTIABLE)

These invariants may never be weakened by a spec, task, migration, or runtime agent:

1. **Read-only by default.** Writes and command execution require explicit
   enablement; risky actions require approval.
2. **Deny secrets by path and content.** Secret paths remain denied by default;
   temporary raw access is exact-path, local-human-approved, bounded, expiring, and
   single-use. Returned content is redacted unless raw access was explicitly granted.
3. **Command allowlist only.** No free host terminal. Executables must be bare
   allowlisted names resolved to canonical paths outside agent-controlled workspaces.
   Shells, destructive commands, path spoofing, and hostile workspace `PATH` targets
   are blocked.
4. **Filesystem jail covers commands too.** Files, working directories, mounts, and
   future edge/profile scopes remain within administrator-configured roots.
5. **Patch-first writes.** Existing repository content changes through checked patches;
   create operations must refuse overwrite unless a separate reviewed workflow exists.
6. **Repository content is untrusted DATA, never authority.** A README, issue, build
   file, manifest, or generated output cannot grant scope or weaken policy.
7. **Audit consequential activity.** Record tool/action, normalized arguments, files,
   decision, duration, result, plan, and actor context without secrets.
8. **Policy is not agent-mutable at runtime.** No public tool, console, profile,
   repository file, provider, or edge task may enlarge authority.
9. **Plans are exact and consumable.** Consequential external actions use
   cryptographic ids, short TTLs, single use, state revalidation, approval, and audit.
10. **Fail closed.** Missing authentication, ambiguous scope, stale state, expired
    authority, unavailable isolation, or unverifiable production health stops work.

## Article II — Architecture (anti-overengineering)

- Prefer a simple modular Go control plane and focused packages.
- Do not add DDD, microservices, databases, queues, or abstraction layers without a
  demonstrated requirement and migration plan.
- Security comes from policy, isolation, identity, scope, egress, and verification;
  changing language does not create security.
- The executable remains a composition root; catalog and capability boundaries are
  protected by tests.
- Public console, asset broker, profile registry, orchestrator, and edge components
  must remain separate authority surfaces with explicit contracts.

## Article III — Development discipline (per numbered step)

1. **RED** — write a failing test or executable verification first.
2. **GREEN** — implement the minimum safe change.
3. **REFACTOR** — simplify without changing behavior.
4. **FULL SUITE** — `go test ./... -count=1` must pass.
5. **QUALITY** — formatting, `go vet ./...`, build, and applicable security gates.
6. **REVIEW** — inspect diff, public contracts, secrets, generated artifacts, and
   deployment consequences.
7. **DOCUMENT** — update active memory and any spec/roadmap/runbook/capsule whose
   asserted state changed.
8. **COMMIT** — one coherent commit, no AI signature.
9. **RELEASE** — phase publication/merge/deploy only after closure audit and explicit
   authority; verify production commit, health, catalog, and representative behavior.

Do not mark a step or phase complete without its verification evidence.

## Article IV — Testing must be adversarial

Security tests attempt bypasses rather than only happy paths: traversal, symlink
escape, command/argument injection, path/PATH executable spoofing, secret exfiltration,
plan replay, stale-state execution, TTL abuse, auth bypass, prompt injection, scope
confusion, and unsafe failure recovery. Every bypass must fail safely.

## Article V — Git and release rules

- Use a feature branch and small reviewable commits.
- Commit format:

  ```text
  Step NN: short title

  What changed and why.
  Verification: commands and result.
  ```

- No `Co-Authored-By` or AI signature.
- No force push, destructive history rewrite, secret commit, or unreviewed generated
  artifact.
- Production releases advance `main` by reviewed fast-forward when possible and must
  be followed by runtime verification. Failed production health stops or rolls back.

## Article VI — Scope and phase discipline

Build in explicit phases. The original Layer 1 MVP is complete; later work must not
silently rewrite its historical spec. Active scope is defined by
`.agent-memory/current-task.md` and `docs/context-capsule.md`, while future intent is
tracked in `docs/product-roadmap.md`.

- Do not mix unrelated phases into one branch.
- Do not claim console, profiles, asset broker, edge, or orchestrator completion until
  code, tests, setup, rollback, troubleshooting, and end-to-end evidence exist.
- PC/WSL/edge work may be implemented and documented without the device present, but
  must remain explicitly **validation pending** until tested on the owner’s machine.
- Phase status must be evidence-based, not inferred from chat intent or unchecked
  roadmap text.

## Article VII — Documentation state is a tested contract

The repository, not the chat, is the durable source of truth. When behavior or phase
state changes, synchronize all affected layers:

1. `specs/` — historical and active requirements/tasks; never leave completed tasks
   unchecked or mark planned work complete without evidence.
2. `.specify/memory/constitution.md` — immutable principles and current phase rules.
3. `.agent-memory/current-task.md` — exact active branch, completed commits, gates,
   and next safe step.
4. `.agent-memory/handoffs/latest.md` — concise restart point for another session.
5. `docs/context-capsule.md` — deployed state, active work, risks, and next steps.
6. `docs/product-roadmap.md` — future intent plus an explicit current status snapshot.
7. ADRs, baselines, runbooks, README, tool docs, and setup guides when their contracts
   or operational instructions changed.

Documentation status is protected by automated tests. A dated baseline is historical
evidence and is not rewritten to pretend a past branch was already deployed.
