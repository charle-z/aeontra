# Current task — P16 Step 1 Edge inventory

Active branch: `p16-global-work-scheduler`
Base: `origin/main` at `cc759e391a1c0b3b4410652267ee374514243ee2`
Step 0 commit: `427ece3930929f3ab41cc37a7ff1f48450d0b9e8`
Status: Step 1 read-only inventory candidate implemented; no installer, migration, Coolify, production, or real Edge mutation has occurred.

## Historical deployed anchors

- P8.1 Console 2.0 is closed and deployed at
  `d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical
  67-tool milestone and `not_paired` Edge state preserved as closure evidence.
- P9 Brain is deployed as the successor to P8.1.
- P13 opaque continuation, P14 authorized HTB actions, and P15 signed Edge are later
  deployed successors.
- Source, VPS deployment, signed release publication, and real Parrot installation
  remain separate evidence boundaries.

## Step 1 implemented cut

`internal/edgelifecycle` now provides a deterministic read-only inventory for:

- preferred state `~/.local/state/mcp-edge`;
- legacy state `~/.config/mcp-devbox-edge`;
- development and HTB roots;
- active signed release link under `/opt/mcp-devbox/releases`;
- explicit historical candidates such as a directory named `p12`.

It classifies state, repository, signed release, unknown directory, file, symlink, and
missing paths. It never creates, moves, renames, repairs, or deletes anything.

Stable blockers cover conflicting identities, occupied preferred state, terminal and
ancestor symlinks, symlinked identity markers, unsafe workspace/lab path types, and
invalid current-release links. A legacy-only valid identity is merely marked as a
migration candidate; migration execution does not exist yet.

## Verification

- `go test ./internal/edgelifecycle -count=1` — PASS
- `go vet ./internal/edgelifecycle` — PASS
- `go test ./cmd/mcp-edge ./packaging/debian ./packaging/parrot -count=1` — PASS
- `go test ./docs -count=1` — PASS before this memory update and must be rerun
- broad `go test ./... -count=1` passed all reported packages through
  `internal/mcpserver/catalog` but the single long process was killed by the connector
  runtime limit; this is not recorded as full-suite green
- the remaining packages from `internal/modelturn` through packaging/profiles were run
  separately and passed
- `git diff --check` — PASS before this memory update and must be rerun

## Next step

Commit the read-only inventory as `Step 1: Inventory legacy Edge installations`, then
add the transactional versioned migration journal and atomic legacy-state move with
failure-injection rollback tests. Do not wire migration into `postinst`, alter the real
Edge, or claim Step 1 complete until that second cut is proven.
