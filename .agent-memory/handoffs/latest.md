# Handoff — P15 documentation truth

Branch `docs/p15-documentation-truth` starts from `origin/main`
`5048a5aa0e0d57d67df3680112aee0d47c954543` (`p15.0.5`).

The remote review proved that the prior local P14 checkout was stale. PR #29 and PR
#38 are merged with all checks green; the source catalog is 98 tools at
`sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12`;
the configured Coolify application tracks `main` and reports healthy.

Evidence boundary: the latest repository-recorded real-host Parrot installation proof
is `p15.0.4`. Do not infer from the `p15.0.5` tag that Parrot installed it, that a
local Git credential was entered, or that a real private-repository smoke passed.
Those require a separate structured update and validation.

Historical deployed anchors required by documentation consistency tests remain
explicit: P8.1 is closed and deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical
67 tools milestone and `not_paired` Edge state. P9 Brain is the deployed successor;
later phases are additive and do not rewrite those baselines.

Documentation changes:

- README identifies the current source/release state, P13 continuation, P14
  first-class authorized HTB actions and the P15 signed Edge line while preserving
  historical P1–P12 markers required by tests.
- `SECURITY.md` and `docs/security.md` now describe profile-specific isolation:
  Layer-1 public commands are not an OS sandbox; ordinary Edge sandbox runtimes use
  mandatory networkless Bubblewrap; trusted Linux workcells share host networking;
  HTB sessions are target/VPN-bound but are not universal egress control.
- `AGENTS.md`, `docs/context-capsule.md`, `docs/documentation-map.md` and
  `.agent-memory/current-task.md` now separate source release, VPS deployment and
  real Edge installation facts.
- `docs/p15_documentation_truth_test.go` locks the current catalog/release and security
  wording while rejecting stale current-state claims.

Verification completed:

- `go test ./docs -count=1` — pass after the final handoff refresh.
- aggregate `go test ./... -count=1` passed every package shown through
  `internal/mcpserver/catalog` before the command runner killed the long process;
  the remaining package batch from `internal/modelturn` through `profiles` passed.
- `go vet ./...` — pass.
- `go build ./...` — pass.
- `git diff --check` — pass after the final handoff refresh.

This documentation step is committed locally on the current branch; use `HEAD` as
the exact commit authority. Do not push, open a PR, deploy or update Parrot unless a
later explicit task requests it.
