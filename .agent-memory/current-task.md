# P2 capability service split

Status: in progress on branch `p2-capability-services`.

Completed:
- Step 54 `2ef5414`: central service core and five capability services.
- Step 55 `b9556a3`: shared jailed workdir resolution moved to the core.

Current Step 56 candidate:
- `RepositoryCapability` now directly owns context packs, directory listing, file reads, code search, patch/create writes, structured memory, handoffs, and persistent notes;
- compile-time assertions prove it implements the catalog's repository read/write, memory, handoff, and notes interfaces;
- context-pack Git status is supplied by the shared `GitCapability` rather than duplicating Git logic;
- read-only repository status helpers moved to `GitCapability` as the minimal dependency required by context packs;
- the aggregate `Service` remains backwards compatible through promoted methods.

Step 56 verification:
- RED failed on all five missing catalog interfaces;
- focused and full tests passed after receiver migration;
- `go vet ./...` and `go build ./...` passed;
- production catalog smoke remains 62 tools with the unchanged hash.

Next autonomous step: migrate the remaining local/remote Git, synchronization, publication, and remote-management methods to `GitCapability`. Do not publish, merge, or deploy P2 without explicit owner approval.
