# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.
- Step 56 `0992d46`: repository/filesystem/memory/notes methods moved to `RepositoryCapability`.

Current Step 57 candidate:
- `GitCapability` now directly owns Git status/diff/commit, clone/fetch, fast-forward, planned publication, remote management, and GitHub HTTPS auth routing;
- compile-time assertions prove it implements all six Git-related catalog interfaces;
- the capability shares the central policy/audit/root/runner/plan core and the configured `SourceCapability` for owner-bound GitHub authentication;
- the aggregate `Service` remains backwards compatible through promoted methods.

Step 57 verification:
- RED failed on all six missing Git catalog interfaces;
- focused and full tests passed after receiver migration;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: migrate GitHub/source-hosting metadata and planned repository creation to `SourceCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
