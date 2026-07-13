# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: P8 closure/tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 1 contract is active. P8 is closed, tagged, deployed, and no longer the
working branch. No Brain implementation or dependency has been added yet.

## Architecture fixed by owner/external review

- Markdown with strict YAML frontmatter is source of truth under
  `MCP_DEVBOX_BRAIN_ROOT`.
- Trust levels: owner-only `curated/` and agent-writable `working/` with provenance
  and mandatory review date.
- Explicit `[[slug]]` links; backlinks derived at index time.
- Pure-Go `modernc.org/sqlite@v1.53.0` FTS5 as a disposable redacted cache.
- Five maximum tools: search, read, write, index, context.
- Dedicated Brain jail; never add Brain root to general repository roots.
- Local Git only, no remote by default or tool-mediated remote operation.
- No resident service, database server, embeddings/vector model, queue, worker, port,
  or new Coolify application on the 4 GB / 2 vCPU VPS.

## Step 1 artifacts

- `specs/006-brain/spec.md`
- `specs/006-brain/threat-model.md`
- `specs/006-brain/plan.md`
- `specs/006-brain/tasks.md`
- `docs/adr/0003-p9-markdown-truth-sqlite-fts5-cache.md`
- `docs/p9_start_test.go`

## Next safe step

Finish and commit Step 1 gates. Then Step 2 begins only with failing tests for strict
frontmatter, curated-write denial, secret rejection, traversal/fuzz, links, review
dates, bounds, symlink safety, and dedicated root jail. Do not add SQLite until Step 4.
