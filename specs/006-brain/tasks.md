# Tasks — P9 Brain

Status: **complete / merge-ready**; production deployment and smoke remain release
gates before the annotated `p9` tag.

- [x] **T01 P9 definition** — independent branch/spec, trust levels, hard bounds,
  no-resident-service invariant, local Git posture, and P10 stop boundary.
- [x] **T02 strict note model** — YAML known-fields parser/renderer, slug/type/author/
  dates/provenance/review/link/size validation, curated/working policy, secret rejection.
- [x] **T03 source store and Git** — dedicated jail, private layout, symlink defense,
  atomic working writes, controlled local commits, rollback, no remote.
- [x] **T04 FTS5 index** — exact pure-Go dependency, schema/probe, full/incremental
  indexing, BM25 search, backlinks, broken links, disposable rebuild.
- [x] **T05 Brain capability** — shared audit/redaction, isolated root, disabled-safe
  configuration, bounded concurrency and close lifecycle.
- [x] **T06 five public tools** — search/read/write/index/context declarative contracts,
  annotations, handlers, docs, original-62 invariance and expected catalog delta.
- [x] **T07 runtime and operations** — `MCP_DEVBOX_BRAIN_ROOT`, persistent volume,
  startup fail-closed, Git/cache backup/update/rollback/troubleshooting, synthetic smoke.
- [x] **T08 P9 release-candidate verification** — full local gates,
  fuzz/concurrency, PR Race/Staticcheck/CodeQL/Dependency Review/Docker/SBOM/Grype,
  and exact 67-tool candidate identity.
- [x] **T09 P9 release-candidate closure** — dated baseline, closure test and
  synchronized sources of truth.
- [ ] **T10 P9 production closure** — merge PR #4 after fresh green checks, configure
  persistent `/brain`, deploy the existing application, verify exact production
  commit/health/catalog/logs, run both smokes, record deployed evidence, and create the
  annotated `p9` tag.
- [ ] **T11 post-P9 console spec** — only after P9 is deployed and tagged, create an
  independent BIOS Operations Console branch/spec; no console, OAuth, Edge or workcell
  implementation belongs to P9.

## Test-first requirements

- curated write denial test before any Brain write implementation;
- secret canary rejection test before persistence;
- slug traversal table and fuzz target before target resolution;
- strict frontmatter/unknown-key tests before parser acceptance;
- index delete/rebuild equivalence before cache is trusted;
- concurrent write/search/reindex test before release;
- catalog contract test proving the previous 62 tools are byte-for-byte unchanged;
- bounded-output tests for search, read backlinks, and context digest.
