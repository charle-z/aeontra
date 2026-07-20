# Handoff — P15 zero-touch local autopilot

Branch: `p15-zero-touch-local-autopilot`
Base: P14 merge `54891fe7bced14e5eacace754f0072ad4d7996c2`.

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

## Active Step 02

Implement the reproducible Debian package, restricted updater, atomic activation,
rollback, repair and P12–P14 migration. Preserve existing identity, keys, workspace
IDs, checkpoints, artifacts, target and authorization; accept no chat-supplied URL,
path, hash, script or command.
