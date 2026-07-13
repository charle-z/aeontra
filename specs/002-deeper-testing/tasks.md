# Tasks — P5 deeper testing

Status: **active**.

- [x] **T01 P5 foundation** — P4 deployment recorded, P5 defined, documentation synchronized, and catalog contract preserved.
- [x] **T02 race baseline** — command and prerequisites recorded; current production builder is blocked by CGO_ENABLED=0, and P6 must execute the real gate with CGO enabled.
- [x] **T03 concurrency stress** — deterministic exactly-once and concurrent-write tests cover grants, action plans, audit JSONL, and OAuth token state; current locking passed without a runtime change.
- [x] **T04 fuzz policy** — path jail, command policy, redaction idempotence, and grant TTL fuzz targets with curated seeds.
- [x] **T05 fuzz protocol/state** — JSON-RPC message/batch and action-plan operation/single-use fuzz targets with curated seeds.
- [x] **T06 coverage gate** — tested coverprofile parser, reproducible CLI, and package-specific security thresholds passing against the full suite.
- [x] **T07 integration matrix** — hermetic stdio/HTTP/auth/catalog/runtime/grant/plan contracts pass with loopback-only synthetic state.
- [ ] **T08 P5 closure** — baseline, documentation synchronization, branch audit, full gates, and release posture.

## Boundary

P5 adds evidence, not authority. No public MCP tool, environment variable, approval
path, transport authentication rule, or deployment target changes in this phase.
