# P9 Brain — Step 7 runtime and operations

Status: Step 7 is complete locally on `p9-brain`. It builds on Step 6 commit
`022c5fadd820e3249b25da62b387147493010105` and P8 closure
`2e3429c9d6342e8e091cadf65293c5c85b1b3259`. The invariant remains no resident service.

## Implemented

- optional `MCP_DEVBOX_BRAIN_ROOT` parsed at startup; empty keeps all 67 tools present
  but uniformly disabled;
- configured root must be absolute, private and disjoint from all repository roots;
- startup initializes/verifies local Git with no remote, opens the private FTS5 cache,
  performs a strict full reindex, attaches Brain, and fails closed on any error;
- tests cover unset/configured behavior, private layout/cache, jail isolation, existing
  source reindex, overlap, malformed truth, remote rejection and safe non-reflective
  errors;
- Dockerfile copies `go.sum`, prepares/chowns `/brain` for UID/GID 10001, and declares
  the dedicated volume without adding a service;
- `docs/runbooks/brain-operations.md` covers Coolify mount/env, owner curation,
  verification, backup, restore, update, rollback and troubleshooting;
- `cmd/brain-smoke` remotely validates exact commit/67-tool hash, index readiness/schema,
  note count and bounded context size without printing credential or private note data;
- coverage: app 71.3%, brain-smoke 76.6%, Brain 81.7%, tools 73.9%, server 82.6%,
  catalog 85.6%.

## Not done yet

- no PR or runner-authoritative Race/Staticcheck/CodeQL/Dependency Review/container
  evidence for P9;
- no production env/volume mutation or deploy;
- no P9 baseline/tag/closure;
- production and console remain P8/62.

## Next exact actions

1. Clean helpers and run final Step 7 local gates, including Docker build/SBOM/Grype if
   available, then commit/publish `Step 7`.
2. Open the P9 release PR and require every remote gate before merge.
3. After green gates, configure the existing app's dedicated `/brain` volume/env,
   merge/deploy, verify exact 67-tool identity and run `brain-smoke`.
4. Keep console UI/auth unchanged throughout P9.
