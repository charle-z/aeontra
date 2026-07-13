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
- Step 36 `f9010db`: privileged profiles.

Current Step 37 candidate:
- added `internal/mcpserver/catalog/platform_core.go` with a narrow `PlatformCoreService` interface;
- moved contiguous `coolify_deploy`, `coolify_list_apps`, `coolify_app_status`, `coolify_deployment_status`, `coolify_app_logs`, and `coolify_create_app` into `RegisterPlatformCore`;
- added focused contract and routing tests.

Compatibility preserved:
- 62 tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- public contracts, aliases, annotations, handlers, approvals, and envs unchanged.

Step 37 verification:
- RED failed because `RegisterPlatformCore` did not exist;
- focused and full tests passed;
- `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed.

Next in the current four-step batch: Step 38 validation-runner platform creation. No publish, merge, or deploy.
