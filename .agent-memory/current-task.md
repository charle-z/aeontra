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
- Step 37 `1f5c057`: core Coolify tools.
- Step 38 `03d1685`: validation-runner platform creation.
- Step 39 `f7380a8`: platform application creation preview.
- Step 40 `758bd0c`: platform deployment planning and force-without-cache execution.
- Step 41 `9853ee2`: platform environment mutation.

Current Step 42 candidate:
- added `internal/mcpserver/catalog/git_reads.go` with a narrow `GitReadService` interface;
- moved `git_status` and `git_diff` into `RegisterGitReads` at their original contiguous catalog positions;
- added focused contract and handler-routing tests.

Compatibility preserved:
- 62 public tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- names, order, descriptions, schemas, versions, annotations, aliases, handlers, approvals, and envs unchanged.

Step 42 verification:
- RED failed because `RegisterGitReads` did not exist;
- focused tests passed;
- the first full suite detected that `tools.Service.GitStatus` retains a variadic compatibility signature; the interface and fake were corrected to match it exactly;
- focused and full tests then passed;
- `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed.

Next in the requested five-step batch: Step 43 Git acquisition. No publish, merge, or deploy.
