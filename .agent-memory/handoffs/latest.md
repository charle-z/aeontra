# Handoff — current production truth

The canonical VPS `main` worktree and production are synchronized at
`0daeb5df0d5e61c1b33aeb363c10aeb0ea91ddf0`.

Verified closure:

- PR #39: documentation truth synchronization, 15/15 checks green, merged.
- PR #40: post-merge documentation closure, 15/15 checks green, merged.
- PRs #41–#44: OAuth popup/callback compatibility, durable OAuth defaults and
  persistent console-session corrections, merged.
- PR #44: 15/15 checks green.
- Coolify application `jqf7qz5ensoqtvl1tb197gcv`: `running:healthy`, repository
  `charle-z/mcp-devbox`, branch `main`.
- Live runtime: version `0.2.0`, protocol `2024-11-05`, 98 tools, catalog hash
  `sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12`.
- Canonical worktree: clean, 0 ahead, 0 behind.

Documentation/security posture:

- README, SECURITY, AGENTS, capsule and documentation map distinguish stable release
  evidence, moving `main`, VPS deployment and real Edge installation.
- Public Layer-1 commands are not an OS sandbox; ordinary Edge sandbox runtimes use
  mandatory networkless Bubblewrap; trusted Linux workcells intentionally share host
  networking; HTB actions remain target/VPN/revision-bound.
- Exact production identity must come from `system_runtime_info`, not a permanently
  hardcoded moving `main` SHA.

Historical deployed successor markers remain explicit: P8.1 is closed and deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical
67 tools and `not_paired` evidence. P9 Brain is deployed as its successor, followed
by later additive P13–P15 phases.

Remaining external evidence boundary: Parrot is only repository-proven at `p15.0.4`.
A `p15.0.5` device update and real private-repository smoke require separate proof.

There is no pending publication or deployment from this closure. Start new work from
current `main`; do not resume the old documentation branches.
