# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.
- Step 56 `0992d46`: repository/filesystem/memory/notes methods moved to `RepositoryCapability`.
- Step 57 `d76a9a0`: Git methods moved to `GitCapability`.
- Step 58 `570f042`: GitHub/source-hosting methods moved to `SourceCapability`.
- Step 59 `6056b5a`: Coolify/platform methods moved to `PlatformCapability`.
- Step 60 `bede1c2`: command/test/sandbox/validation/privileged methods moved to `ExecutionCapability`.

Current Step 61 candidate:
- added an AST boundary test that rejects operational methods on the aggregate `Service` facade and direct facade mutation of capability/core state;
- all public `With...` configuration methods now delegate to the owning shared core or capability;
- policy, audit, root, command runner, and action-plan store remain centralized in `serviceCore`;
- source, platform, Git, and execution-specific dependencies are configured only by their owning capability;
- existing fluent configuration API remains unchanged.

Step 61 verification:
- RED reported every direct facade mutation;
- the focused boundary test passed after delegation was introduced;
- full tests, `go vet ./...`, and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: P2 documentation/baseline closure, branch-vs-main audit, final gates, and merge-readiness verdict. Do not publish, merge, or deploy P2 without explicit owner approval.
