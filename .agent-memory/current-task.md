# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed commits:
- Step 28 `0ba9f52`: introduced `internal/mcpserver/catalog` and moved `system_runtime_info` into `RegisterRuntime`.
- Step 29 `cda9d37`: moved `notes_list`, `notes_read`, `notes_write_preview`, and `notes_write` behind a narrow `NotesService` interface.
- Step 30 `6be7af2`: moved contiguous `memory_read` and `memory_write` behind a narrow `MemoryService` interface.
- Step 31 `2b16807`: moved `memory_update_handoff` behind a narrow `HandoffService` interface.
- Step 32 `e33d376`: moved `run_tests`, `project_validation_preview`, and `project_validation_execute` behind a narrow `ValidationService` interface.
- Step 33 `da8074c`: moved `build_context_pack`, `list_dir`, `read_file`, `read_many_files`, and `search_code` behind a narrow `RepositoryReadService` interface.

Current Step 34 candidate:
- added `internal/mcpserver/catalog/repository_writes.go` with a narrow `RepositoryWriteService` interface;
- moved the contiguous `apply_patch` and `create_file` registrations into `RegisterRepositoryWrites` at their original catalog position;
- added focused module tests for names, order, descriptions, schemas, versions, and handler routing.

Compatibility preserved across all cuts:
- 62 public tools;
- deterministic catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- same names, order, descriptions, schemas, versions, annotations, handlers, aliases, approval posture, and environment variables.

Step 34 verification:
- RED focused test failed before implementation because `RegisterRepositoryWrites` did not exist;
- `go test ./internal/mcpserver/catalog -count=1` passed;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `mcp-catalog-smoke` matched production commit `3d161352b1d24670b07f48155f1eddc6370af8fd`, tool count 62, and the deterministic catalog hash.

Capabilities decision: defer a generic capability dispatcher until P1-P3 are stable. Reassess when catalog growth/change frequency justifies the additional dispatcher, discovery, schema, audit, and policy complexity.

Next recommended domain: command and sandbox tools, starting with a small contiguous group and one commit. Do not publish, merge, or deploy until several stable groups are complete and explicitly approved.
