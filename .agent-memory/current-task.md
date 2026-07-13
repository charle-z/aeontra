# P1 tool catalog modularization

Status: in progress on branch `p1-tool-catalog-runtime`.

Completed commits:
- Step 28 `0ba9f52`: runtime.
- Step 29 `cda9d37`: notes.
- Step 30 `6be7af2`: structured memory.
- Step 31 `2b16807`: handoff memory.
- Step 32 `e33d376`: tests and project validation.
- Step 33 `da8074c`: repository reads.
- Step 34 `a61f8df`: repository writes.

Current Step 35 candidate:
- added `internal/mcpserver/catalog/execution.go` with a narrow `ExecutionService` interface;
- moved contiguous `run_command`, `sandbox_status`, and `sandbox_exec` into `RegisterExecution` at their original position;
- added focused tests for names, order, descriptions, schemas, versions, validation of empty commands, and handler routing.

Compatibility preserved:
- 62 tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- names, order, descriptions, schemas, versions, annotations, handlers, aliases, approvals, and envs unchanged.

Step 35 verification:
- RED failed because `RegisterExecution` did not exist;
- focused tests passed;
- full tests initially caught and then resolved the now-unused `fmt` import;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and production catalog smoke passed.

Next in the current four-step batch: Step 36 privileged profiles, Step 37 core Coolify tools, Step 38 validation-runner platform creation. No publish, merge, or deploy.
