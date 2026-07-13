# Tasks — P5 deeper testing

Status: **active**.

- [x] **T01 P5 foundation** — P4 deployment recorded, P5 defined, documentation synchronized, and catalog contract preserved.
- [ ] **T02 race baseline** — run full race detector and record platform prerequisites/results.
- [ ] **T03 concurrency stress** — add deterministic tests for shared policy, plans, audit, OAuth, and HTTP state.
- [ ] **T04 fuzz policy** — path jail, command policy, and redaction fuzz targets with curated seeds.
- [ ] **T05 fuzz protocol/state** — JSON-RPC batches, grants, and action-plan invariant fuzz targets.
- [ ] **T06 coverage gate** — tested coverprofile parser and package-specific security thresholds.
- [ ] **T07 integration matrix** — stdio/HTTP/auth/catalog/grants/plans/runtime synthetic integration tests.
- [ ] **T08 P5 closure** — baseline, documentation synchronization, branch audit, full gates, and release posture.

## Boundary

P5 adds evidence, not authority. No public MCP tool, environment variable, approval
path, transport authentication rule, or deployment target changes in this phase.
