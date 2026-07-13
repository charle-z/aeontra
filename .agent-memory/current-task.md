# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed commits:
- Step 28 `0ba9f52`: introduced `internal/mcpserver/catalog` and moved `system_runtime_info` into `RegisterRuntime`.
- Step 29 `cda9d37`: added shared schema helpers and moved `notes_list`, `notes_read`, `notes_write_preview`, and `notes_write` behind a narrow `NotesService` interface.

Current Step 30 candidate:
- added a narrow `MemoryService` interface;
- moved contiguous `memory_read` and `memory_write` registrations into `RegisterMemory`;
- left `memory_update_handoff` in place to preserve catalog order for a later focused cut.

Compatibility preserved across all cuts:
- 62 public tools;
- deterministic catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- same names, order, descriptions, schemas, versions, annotations, handlers, aliases, approval posture, and environment variables.

Step 30 verification:
- RED test failed before implementation;
- focused catalog tests passed;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `mcp-catalog-smoke` matched production count/hash.

Capabilities decision: defer a generic capability dispatcher until P1-P3 are stable. It may become valuable when the catalog changes frequently or grows substantially, but implementing it now would concentrate authority and duplicate schema/policy/audit complexity.

Next recommended cut: `memory_update_handoff` as its own registrar at the same catalog position, then move validation or repository-read domains.
