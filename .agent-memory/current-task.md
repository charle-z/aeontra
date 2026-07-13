# P2 capability service split

Status: in progress on branch `p2-capability-services` from deployed `main` commit `0de426e088466a1421b527f8ce1bf83cb53bd2a9`.

Production/client baseline:
- runtime commit `0de426e088466a1421b527f8ce1bf83cb53bd2a9`;
- 62 public tools;
- catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- this ChatGPT session sees the refreshed catalog.

Current Step 54 candidate:
- added one central `serviceCore` containing policy, audit, root, command runner, and action-plan store;
- added `RepositoryCapability`, `GitCapability`, `SourceCapability`, `PlatformCapability`, and `ExecutionCapability`;
- changed `Service` into a backwards-compatible facade that embeds the five capability services;
- all capabilities share the exact same core and plan store;
- configuration methods continue to update the owning capability without changing callers;
- moved shared redaction onto the central core.

Step 54 verification:
- RED failed because the capability/core types did not exist;
- focused `internal/tools` tests passed after implementation;
- `go test ./... -count=1`, `go vet ./...`, and `go build ./...` passed;
- production catalog smoke still reports 62 tools and the unchanged hash.

Next autonomous step: move shared path/workdir helpers onto `serviceCore`, then migrate repository/filesystem/memory/notes method receivers to `RepositoryCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
