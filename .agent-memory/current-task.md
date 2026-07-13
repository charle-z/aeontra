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
- Step 35 `2c5a073`: command and sandbox execution.

Current Step 36 candidate:
- added `internal/mcpserver/catalog/privileged.go` with a narrow `PrivilegedService` interface;
- moved `privileged_task_preview` and `privileged_task_execute` into `RegisterPrivileged` at their original position;
- added focused tests for names, order, descriptions, schemas, versions, and handler routing.

Compatibility preserved:
- 62 tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- public contracts, annotations, handlers, approvals, aliases, and envs unchanged.

Step 36 verification:
- RED failed because `RegisterPrivileged` did not exist;
- focused tests passed;
- full tests detected temporary transform helpers and passed after those non-product files were removed;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and production catalog smoke passed.

Next in the current four-step batch: Step 37 core Coolify tools and Step 38 validation-runner platform creation. No publish, merge, or deploy.
