# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.
- Step 56 `0992d46`: repository/filesystem/memory/notes methods moved to `RepositoryCapability`.
- Step 57 `d76a9a0`: Git reads, commits, synchronization, publication, and remote management moved to `GitCapability`.

Current Step 58 candidate:
- `SourceCapability` now owns GitHub API repository lookup/creation helpers and the planned source repository info/create workflow;
- compile-time assertions prove it implements the source repository creation and metadata catalog interfaces;
- `GitCapability` and `PlatformCapability` continue sharing this exact configured source capability rather than copying tokens or owner state;
- the aggregate `Service` remains backwards compatible through promoted methods.

Step 58 verification:
- RED failed on both missing source-hosting catalog interfaces;
- focused and full tests passed after receiver migration;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: migrate legacy Coolify, planned platform application/deployment, force deployment, and managed validation-runner platform methods to `PlatformCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
