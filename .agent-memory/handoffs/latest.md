# Handoff — P15 zero-touch local autopilot

Branch: `p15-zero-touch-local-autopilot`
Base: P14 merge `54891fe7bced14e5eacace754f0072ad4d7996c2`.

Historical verified foundation: P8.1 is closed and deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with the verified
67 tools milestone and safe `not_paired` Edge state. P15 is additive.

Current P15 status: Steps 01-06 are committed. Exact-head Linux CI, PR merge,
automatic deployment and structured Parrot migration remain pending and are not
claimed by this handoff.

## Completed Step 01

- `internal/bundle` defines a strict version-1 Ed25519 manifest and fixed release
  layout for Edge, model driver, autopilot worker, provider, HTB actions and systemd.
- Verification binds release, exact commit, protocol, catalog hash and architecture;
  all components must be regular non-symlink files below the release root.
- Closed safe errors are `bundle_mismatch`, `provider_outdated`, `driver_outdated`
  and `manifest_invalid`.
- `mcp-bundle-manifest` hashes a staged release and creates new manifest/signature
  files using an external raw Ed25519 private key. It never overwrites files.
- Packaged `mcp-edge opencode` verifies the compiled bundle identity before polling
  for a new runtime. Unstamped local builds fail closed.
- The systemd unit and onboarding preflight require `/opt/mcp-devbox/current`, its
  manifest/signature and the autopilot worker.
- Architecture, security, operations and context documentation are updated.

## Verification

- `go test ./internal/bundle ./cmd/mcp-bundle-manifest -count=1` -> pass.
- `go test ./internal/bundle ./packaging/parrot -count=1` with Git Bash -> pass.
- Linux cross `go vet ./...` -> pass.
- Linux cross `go build ./...` -> pass.
- `git diff --check` -> pass.
- Full executable suite is not claimed locally: this Windows host cannot execute the
  repository's Linux-only `syscall.Statfs`/Bubblewrap packages. Exact-head CI Linux
  remains mandatory.

## Completed Step 02

- reproducible Debian content builder plus detached GPG signature and SHA-256;
- deterministic signed update archive/channel publisher;
- official-only bounded downloader with strict tar extraction;
- flock-serialized staging, signed verification, atomic `current`/`previous`, exact
  unit install, Edge-only restart, health rollback and conservative signed cleanup;
- root-only updater accepting only `status`, `update stable`, `rollback`, `repair`;
- repair of exact official modes/links/unit/service, with P14 backups;
- package `postinst` rollback, preferred/legacy state preservation and no ID rewrite;
- one `mcp-edge onboard --server` action plus a systemd path unit that starts Edge
  when identity appears.

Portable bundle/Debian/Parrot tests, shell syntax, Linux vet/build and diff checks
pass. Linux-only updater transaction tests compile but need exact-head CI execution.

## Active Step 03

Implement closed control-plane lab preparation and retarget tasks. All target/VPN/
LHOST/path/Git/contract/inventory work happens on Edge. Reuse machine workspaces and
IDs, preserve evidence/checkpoints, invalidate sessions and increment authorization
revision on retarget.
