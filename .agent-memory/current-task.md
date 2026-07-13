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
- Step 42 `04dba6c`: Git reads.
- Step 43 `dd421b7`: Git acquisition.
- Step 44 `f7b5b41`: Git fast-forward.

Current Step 45 candidate:
- added `internal/mcpserver/catalog/git_publication.go` with a narrow `GitPublicationService` interface;
- moved `git_push` and `repo_publish_preview` into `RegisterGitPublication` while preserving their historical catalog order;
- added focused contract and handler-routing tests.

Compatibility preserved:
- 62 public tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- names, order, descriptions, schemas, versions, annotations, aliases, handlers, approvals, and envs unchanged.

Step 45 verification:
- RED failed because `RegisterGitPublication` did not exist;
- focused and full tests passed;
- `go vet ./...`, `go build ./...`, diff review, and production catalog smoke passed.

Next in the requested five-step batch: Step 46 source repository creation, Step 47 source repository info, Step 48 remote management. No publish, merge, or deploy.
