# Plan — P9 Brain

Status: **complete / merge-ready**; Step 8 production verification remains mandatory
before the annotated `p9` tag.

## Delivery sequence

1. **Step 1 — Contract and architecture**
   - independent branch/spec/threat model/tasks;
   - ADR 0003 for Markdown truth + pure-Go SQLite FTS5 cache;
   - hard bounds, trust levels, local Git posture, and no-resident-service invariant;
   - documentation RED test.

2. **Step 2 — Strict note model and store jail**
   - validated config/root layout and dedicated jail;
   - strict YAML frontmatter parser/renderer;
   - slug, author, type, timestamp, review, size, and link validation;
   - curated/working trust checks;
   - secret-canary rejection and read redaction;
   - traversal/symlink adversarial tests and slug fuzz target.

3. **Step 3 — Local Git source history**
   - initialize a private local repository and cache ignore rule;
   - fixed local-only Git argv/environment with hooks disabled;
   - atomic working note upsert and same-author update;
   - one successful write -> one local commit;
   - rollback source/index state on Git failure;
   - no remote operations.

4. **Step 4 — FTS5 index and backlinks**
   - add exact `modernc.org/sqlite` dependency;
   - schema/version/FTS probe;
   - bounded full rebuild from Markdown truth;
   - incremental metadata/FTS/link transaction;
   - BM25 plain-text search, bounded snippets, backlinks, broken-link status;
   - delete/rebuild equivalence and concurrent index tests.

5. **Step 5 — Brain capability and five tools**
   - separate `BrainCapability` sharing audit/redaction but not repository roots;
   - disabled-safe behavior when env is unset;
   - `brain_search`, `brain_read`, `brain_write`, `brain_index`, `brain_context`;
   - declarative catalog schemas/annotations/order;
   - original 62 contract snapshot unchanged plus expected five-tool delta;
   - initialization instructions mention demand-driven Brain usage without injecting data.

6. **Step 6 — Runtime configuration and operations**
   - `MCP_DEVBOX_BRAIN_ROOT` startup plumbing;
   - persistent-volume, permissions, Git, cache rebuild, backup, update, rollback, and
     troubleshooting guide;
   - startup fail-closed when configured Brain is invalid;
   - runtime close and smoke/status diagnostics without private paths/content.

7. **Step 7 — Full verification and release candidate**
   - ordinary/focused/concurrency/fuzz tests;
   - atomic coverage and package threshold for `internal/brain`;
   - vet/build/actionlint/Govulncheck;
   - PR, Race, Staticcheck, CodeQL, Dependency Review, Docker/SBOM/Grype;
   - no secret/private Brain content in CI artifacts or logs.

8. **Step 8 — Deployment and closure**
   - configure existing application volume/env only; no new application/service;
   - merge through green PR;
   - exact production identity and 67-tool catalog smoke;
   - bounded synthetic Brain smoke using non-sensitive fixtures;
   - dated baseline, closure test, synchronized docs, annotated `p9` tag;
   - only then write P10 Layer 2/3 egress spec, with no implementation.

## Rollback

- Before merge: revert the current Step commit on `p9-brain`; never force push.
- Runtime implementation: deploying tag `p8` removes all Brain tools/runtime wiring.
- Data: Markdown and local Git remain on the persistent Brain volume; deleting
  `.cache/brain.db` is always safe and reindexable.
- Dependency: removing the Brain package and `modernc.org/sqlite` restores the P8
  dependency graph; no external database migration exists.
- Deployment: unset `MCP_DEVBOX_BRAIN_ROOT` and deploy a P8 commit. Never delete the
  persistent Brain volume as part of application rollback.

## Verification rules

- Every Step begins with a failing focused test or documentation invariant.
- Every Step ends with focused tests, full suite, diff review, docs/memory update, and
  a `Step N:` commit.
- No implementation Step starts while the prior Step is uncommitted or has a red gate.
- PR and post-merge gates are mandatory; no direct commits to `main`.
