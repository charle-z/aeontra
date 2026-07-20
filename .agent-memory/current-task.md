# Current task — P15 zero-touch local autopilot

Branch: `p15-zero-touch-local-autopilot`
Base: `origin/main` at P14 merge `54891fe7bced14e5eacace754f0072ad4d7996c2`.

Historical verified foundation: P8.1 is closed and deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with the verified
67 tools milestone and safe `not_paired` Edge state. P15 work is additive.

Current P15 status: Steps 01-06 are committed. Exact-head Linux CI, PR merge,
automatic deployment and structured Parrot migration remain pending and are not
claimed by this local handoff.

P14 is the preserved foundation: target-locked local HTB actions, opaque sessions,
local credential handles, local-only sensitive results, OpenCode integration, and
registered VPN/target metadata. P15 must close the operator workflow without weakening
those boundaries.

Completed Step 01 — signed indivisible Edge bundles:

1. Define one versioned manifest covering Edge, model driver, autopilot worker,
   provider files, HTB actions, systemd unit, protocol, catalog, commit and architecture.
2. Sign and verify the canonical manifest with Ed25519.
3. Hash every required regular non-symlink component inside the release root.
4. Fail before a runtime starts with only the safe precise codes `bundle_mismatch`,
   `provider_outdated`, `driver_outdated`, or `manifest_invalid`.
5. Add the release builder and wire verification into the persistent Edge service.

Verification executed with an official temporary Go 1.26.5 SDK and private temporary
caches: bundle and Parrot packaging tests pass; Linux `go vet ./...`, Linux
`go build ./...`, and `git diff --check` pass. The Windows host cannot execute the
Linux-only full suite (`syscall.Statfs` and Bubblewrap paths); exact-head CI Linux
remains mandatory and no local pass is claimed for that suite.

Completed Step 02 — atomic Parrot installer and updater:

1. Build the reproducible Debian filesystem/package layout around a pre-signed bundle.
2. Add the root-owned closed updater with staging, atomic `current`, health check,
   previous-release rollback and conservative cleanup.
3. Add repair and P12–P14 migration that preserve identity, keys, workspaces,
   contracts, checkpoints, artifacts, target authorization and opaque IDs.
4. Add clean-install, migration, repeat-install, atomicity, rollback and restricted
   authority tests before exposing update controls.

Verification: portable bundle/Debian/Parrot contract tests and every shell syntax
check pass; Linux cross `go vet ./...`, `go build ./...`, and `git diff --check`
pass. Linux-only updater transaction tests compile and remain mandatory in exact-head
CI, because this Windows host cannot execute Unix symlink/flock tests.

Active Step 03 — automatic lab preparation and retargeting:

1. Add closed durable Edge task kinds for HTB prepare and retarget metadata only.
2. Execute preparation locally: private target validation, VPN route/LHOST discovery,
   private Git workspace/README/directories, idempotent registry/profile/inventory and
   same machine workspace ID reuse across IP changes.
3. Retarget in place while preserving evidence/checkpoint, invalidating sessions and
   incrementing a durable authorization revision.
4. Expose only `workspace_lab_prepare` and `workspace_lab_retarget` through the remote
   control plane and return opaque safe status/IDs.
