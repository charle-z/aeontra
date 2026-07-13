# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed commits:
- Step 28 `0ba9f52`: introduced `internal/mcpserver/catalog` and moved `system_runtime_info` into `RegisterRuntime`.
- Step 29 `cda9d37`: moved `notes_list`, `notes_read`, `notes_write_preview`, and `notes_write` behind a narrow `NotesService` interface.
- Step 30 `6be7af2`: moved contiguous `memory_read` and `memory_write` behind a narrow `MemoryService` interface.

Current Step 31 candidate:
- added a narrow `HandoffService` interface;
- moved `memory_update_handoff` into `RegisterHandoff` at its original catalog position.

Compatibility preserved across all cuts:
- 62 public tools;
- deterministic catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- same names, order, descriptions, schemas, versions, annotations, handlers, aliases, approval posture, and environment variables.

Step 31 verification:
- RED test failed before implementation;
- focused catalog test passed;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `mcp-catalog-smoke` matched production count/hash.

Capabilities decision: defer a generic capability dispatcher until P1-P3 are stable. Reassess when catalog growth/change frequency justifies the additional dispatcher, discovery, schema, audit, and policy complexity.

Next recommended domain: validation tools (`run_tests`, `project_validation_preview`, `project_validation_execute`) or repository read tools, one contiguous group per commit.
