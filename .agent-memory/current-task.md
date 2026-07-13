# P9 Brain — Step 2 note model and store jail

Status: Step 2 implementation is complete locally on `p9-brain`. The resource
invariant remains no resident service. This work is based on Step 1 commit `9e2ca7202f5776f4afbe140eb89f65984ce4b26e` and P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`.

## Implemented

- `internal/brain/model.go`: strict known-fields YAML parser, deterministic renderer,
  Note/Metadata/AgentDraft types, five note types, owner/agent author rules, canonical
  UTC timestamps, review dates, provenance/title/body/file bounds, server-owned write
  timestamps, same-author updates, and explicit expired state.
- Strict ASCII kebab-case slug grammar and validated sorted unique `[[slug]]` links.
- Existing policy secret scanner rejects agent drafts before rendering and redacts
  manual source reads defensively.
- `internal/brain/store.go`: independent absolute jail, private 0700 layout for root,
  curated/working/cache, symlink-ancestor/source rejection, broad-permission denial,
  global curated/working slug uniqueness, redacted source reads, and dynamic clock.
- Tests include curated-target denial, secret canary, traversal/path/Unicode table,
  fuzz seeds, malformed/unknown/duplicate YAML, dates, trust, bounds, symlinks,
  permissions, duplicates, redaction, and deterministic round trip.
- `internal/brain` coverage 82.9% with an 80% gate.
- `go.yaml.in/yaml/v3` is now direct because production code uses it.

## Not implemented yet

- no source writes or local Git initialization/commits;
- no SQLite dependency, cache, FTS5, search, backlinks, or reindex;
- no Brain capability, MCP tools, runtime env, persistent mount, or deployment;
- existing 62-tool catalog remains unchanged.

## Next exact actions

1. Run final Step 2 gates, clean helpers, commit/publish as `Step 2`.
2. Begin Step 3 with RED tests for private Git initialization, no remotes/hooks,
   atomic working upsert, one local commit, source/index rollback, and no curated write.
3. Do not add `modernc.org/sqlite` until Step 4.
