# Tasks — P9 Brain

Status: **active**.

- [x] **T01 P9 definition** — independent branch/spec, trust levels, hard bounds,
  no-resident-service invariant, local Git posture, and P10 stop boundary.
- [x] **T02 strict note model** — YAML known-fields parser/renderer, slug/type/author/
  dates/provenance/review/link/size validation, curated/working policy, secret rejection.
- [ ] **T03 source store and Git** — dedicated jail, private layout, symlink defense,
  atomic working writes, controlled local commits, rollback, no remote.
- [ ] **T04 FTS5 index** — exact pure-Go dependency, schema/probe, full/incremental
  indexing, BM25 search, backlinks, broken links, disposable rebuild.
- [ ] **T05 Brain capability** — shared audit/redaction, isolated root, disabled-safe
  configuration, bounded concurrency and close lifecycle.
- [ ] **T06 five public tools** — search/read/write/index/context declarative contracts,
  annotations, handlers, docs, original-62 invariance and expected catalog delta.
- [ ] **T07 runtime and operations** — `MCP_DEVBOX_BRAIN_ROOT`, persistent volume,
  startup fail-closed, Git/cache backup/update/rollback/troubleshooting, synthetic smoke.
- [ ] **T08 P9 verification** — full local gates, fuzz/concurrency, PR Race/Staticcheck/
  CodeQL/Dependency Review/Docker/SBOM/Grype, exact production identity and 67 tools.
- [ ] **T09 P9 closure** — dated baseline, closure test, synchronized sources of truth,
  annotated `p9` tag.
- [ ] **T10 P10 spec only** — after P9 closure, create `specs/007-layer-2-egress/`;
  implementation remains owner-decision pending.

## Test-first requirements

- curated write denial test before any Brain write implementation;
- secret canary rejection test before persistence;
- slug traversal table and fuzz target before target resolution;
- strict frontmatter/unknown-key tests before parser acceptance;
- index delete/rebuild equivalence before cache is trusted;
- concurrent write/search/reindex test before release;
- catalog contract test proving the previous 62 tools are byte-for-byte unchanged;
- bounded-output tests for search, read backlinks, and context digest.
