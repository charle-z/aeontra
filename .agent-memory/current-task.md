# Current task — P16 Step 1 completed, Step 2 pending

Active branch: `p16-global-work-scheduler`
Base: `origin/main` at `cc759e391a1c0b3b4410652267ee374514243ee2`
Step 0 commit: `427ece3930929f3ab41cc37a7ff1f48450d0b9e8`
Step 1 inventory commit: `03dff55b34660c1e72d44e133cbf30bba3fe33c5`
Status: Step 1 transactional migration candidate validated locally and ready to commit; no `postinst`, Coolify, production, or real Edge mutation has occurred.

## Historical deployed anchors

- P8.1 Console 2.0 is closed and deployed at
  `d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical
  67-tool milestone and `not_paired` Edge state preserved as closure evidence.
- P9 Brain is deployed as the successor to P8.1.
- P13 opaque continuation, P14 authorized HTB actions, and P15 signed Edge are later
  deployed successors.
- Source, VPS deployment, signed release publication, and real Parrot installation
  remain separate evidence boundaries.

## Step 1 delivered

- Read-only P12/P15 inventory for preferred/legacy state, project roots, signed release,
  fixed known systemd units, repositories, releases, and explicit historical candidates.
- Unknown directories such as `~/p12` are classified but never moved, renamed, repaired,
  or deleted.
- Stable blockers for symlinks/ancestors, identity-marker links, conflicting/occupied
  state, unsafe root types, signed-release errors, and unsafe systemd roots.
- Versioned private migration journal for only
  `~/.config/mcp-devbox-edge -> ~/.local/state/mcp-edge`.
- Exact-plan revalidation, valid identity/private-key loading, owner/mode checks, and
  execution as the Edge user.
- Linux `renameat2(..., RENAME_NOREPLACE)`, fsync, verification, injected-failure
  rollback, and crash recovery without duplicate migration or identity regeneration.
- Closed local CLI: `mcp-edge lifecycle inspect|migrate-state|recover-state`; no paths,
  URLs, commands, scripts, IDs, or free-form parameters.
- Safe local output containing only path kinds, migration state, and stable blocker/status
  codes.
- Authoritative project documentation in `docs/edge-lifecycle-migration.md` and the P16
  task/documentation map.

Windows-mount/dirty/mismatched/ambiguous project checkout validation remains explicitly
assigned to Step 3, where the project workspace resolver has repository/mount context.

## Verification

- `go test ./internal/edgelifecycle ./cmd/mcp-edge ./packaging/debian ./packaging/parrot ./docs -count=1` — PASS
- `go vet ./internal/edgelifecycle ./cmd/mcp-edge` — PASS
- expanded Edge regression across `internal/edge`, `internal/edgeclient`, bundle,
  edgeupdate, package and lifecycle — PASS
- `git diff --check` — PASS before this memory update and must be rerun
- local `-race` remains unavailable because this MCP environment has CGO disabled; race
  remains a blocking remote gate, not a local green claim

## Next step

Commit `Step 1: Add transactional Edge state migration`. Then start Step 2 test-first:
replace the direct shell state move in `packaging/debian/postinst.in` with the closed
recover/plan/apply lifecycle executed as the Edge user, preserve valid identity, skip
pairing when already paired, and prove package upgrade/rollback in fixtures before any
real Edge installation.
