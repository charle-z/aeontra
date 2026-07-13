# P9 Brain — Step 3 local Git history and atomic writes

Status: Step 3 is complete locally on `p9-brain`. It builds on Step 2 commit
`fd810aad507ef118570a5097b40945f7138a57df` and P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`. The invariant remains no resident service.

## Implemented

- `internal/brain/git.go` resolves a fixed absolute Git executable outside the Brain
  root and runs it directly with a stripped environment, no shell, prompts disabled,
  global/system config disabled, hooks redirected to the null device, filters bypassed,
  protocol access denied, and bounded output.
- Local repository initialization creates/validates private `.git/` and `.gitignore`,
  rejects symlinks/unsafe ignore files/existing remotes, creates one bootstrap commit,
  and is idempotent.
- Writes use Git plumbing: `read-tree`, `hash-object --no-filters`, `update-index`,
  `write-tree`, `commit-tree`, and compare-and-swap `update-ref`.
- `internal/brain/write.go` serializes working-note writes, rejects curated duplicates,
  preserves same-author ownership and created timestamps, writes mode 0600 atomically,
  creates exactly one local commit, and restores source/index state when Git fails.
- A bounded independent critical context avoids ambiguous client cancellation after
  source mutation. If `update-ref` reports an error after applying, HEAD is verified
  independently before deciding success versus rollback.
- Tests cover hooks, remotes, metadata swaps, unsafe ignore files, secret/cancelled/
  cross-author writes, commit/ref failures, ambiguous ref results, concurrency, clean
  history, generic errors, and allowed command surface.
- `internal/brain` coverage is 80.5% against an 80% gate.

## Not implemented yet

- no SQLite dependency, cache schema, FTS5, search, backlinks, or reindex;
- no Brain capability, MCP tools, runtime env, persistent mount, or deployment;
- existing 62-tool catalog remains unchanged.

## Next exact actions

1. Run final Step 3 gates, clean helpers, commit/publish as `Step 3`.
2. Begin Step 4 with RED tests for FTS5 availability, schema/version probe, bounded
   full reindex, incremental update, BM25 plain-text search, links/backlinks, broken
   links, deletion/rebuild equivalence, and concurrent read/reindex/write behavior.
3. Add exact `modernc.org/sqlite@v1.53.0` only after the FTS5 RED test exists.
