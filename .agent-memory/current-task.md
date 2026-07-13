# P9 Brain — Step 1 contract

Status: active on branch `p9-brain`, based exactly on deployed/tagged P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`.

## Current scope

- `specs/006-brain/spec.md` defines Markdown/frontmatter truth, curated/working trust,
  five bounded tools, hard limits, local Git history, dedicated jail, and no full
  context injection.
- `specs/006-brain/threat-model.md` maps traversal, curated writes, secrets, drift,
  cache authority, FTS injection, concurrency, Git hooks/remotes, and resource abuse
  to explicit controls and stop conditions.
- `specs/006-brain/plan.md` is test-first and separates note/store/Git/index/tools/
  runtime/release steps with rollback.
- `specs/006-brain/tasks.md` keeps implementation tasks open after T01.
- ADR 0003 accepts Markdown truth plus `modernc.org/sqlite@v1.53.0` FTS5 cache and
  rejects embeddings/resident services for the 4 GB RAM / 2 vCPU VPS.
- `docs/p9_start_test.go` protects the initial contract and current-state documents.

## Invariants

- `MCP_DEVBOX_BRAIN_ROOT` is a dedicated jailed persistent volume, never a general
  repository root.
- `curated/` is owner-only; agent tools write only `working/` with provenance and
  review dates.
- Files are truth; SQLite is disposable/redacted cache; local Git has no remote.
- Exactly five future Brain tools; the existing 62 contracts remain unchanged.
- No resident service, database server, embeddings/vector model, queue, worker, port,
  or new Coolify application.
- P10 implementation is forbidden until P9 is closed/tagged.

## Next exact actions

1. Make the Step 1 documentation RED test green and run the full local gates.
2. Commit/publish Step 1 on `p9-brain`; no PR required until an implementation release
   candidate, but no later Step begins with a dirty/uncommitted Step 1.
3. Begin Step 2 with RED tests for curated-write denial, secret canaries, strict YAML,
   slugs/traversal/fuzz, links, dates, bounds, and dedicated jail.
4. Do not add `modernc.org/sqlite` until Step 4 has a failing FTS5 test.
