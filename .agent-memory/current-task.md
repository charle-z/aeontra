# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: introduced one central `serviceCore` and five capability services behind the backwards-compatible `Service` facade.

Current Step 55 candidate:
- moved `workdir` from the aggregate `Service` receiver to `serviceCore`;
- verified that repository, Git, source, platform, and execution capabilities all resolve paths through the exact same policy/root helper;
- no public behavior or catalog metadata changed.

Step 55 verification:
- RED failed because capability services did not expose the shared workdir helper;
- focused and full tests passed;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke still reports 62 tools and the unchanged deterministic hash.

Next autonomous step: make `RepositoryCapability` directly own repository/filesystem/memory/notes methods while delegating context-pack Git status to the shared `GitCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
