# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: Step 1 `9e2ca7202f5776f4afbe140eb89f65984ce4b26e` / P8 tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 2 is implemented locally and awaiting final gates/commit. No Git,
SQLite, MCP tool, runtime configuration, volume, or deployment change exists yet.

## Step 2 security behavior

- strict known-fields YAML frontmatter and deterministic Markdown rendering;
- curated owner-only and working agent-author validation;
- mandatory provenance/review dates and server-owned timestamps for agent drafts;
- hard slug/title/provenance/body/file bounds and validated `[[slug]]` links;
- secret-shaped agent content rejected before persistence; manual reads redacted;
- dedicated Brain jail outside general repository roots;
- private 0700 layout, regular private source files, symlink/ancestor defense;
- global curated/working slug uniqueness;
- traversal and link fuzz seeds;
- 82.9% package coverage with an 80% gate.

## Next safe step

Commit/publish Step 2 after full gates. Step 3 begins with failing tests for controlled
local Git initialization and atomic working-note writes/rollback. Do not add SQLite,
register tools, or wire runtime configuration before their planned steps. The
invariant is no resident service.
