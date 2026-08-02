# Handoff — safe Edge checkout synchronization candidate

Hito 3B is merged through PR #126 at `1d511cb038141a2a7f4bdf97d2472d4428e1f8d1`,
deployed with 121 tools, and reconciled through the stable Front Door. Production OAuth
discovery and the unauthenticated MCP challenge are healthy. Brain note
`gpt-web-direct-edge-h3b-deployed` records the exact deployments and remaining real
Edge acceptance.

Current branch `codex/edge-safe-sync` is based exactly on that merge. It adds four
closed direct-Edge Git operations for status, no-tag fetch, exact fast-forward preview
and plan execution. The implementation reuses the existing private owner-bound
askpass runner. Callers cannot provide paths, URLs, remotes, refspecs, tags, force or
checkout/reset actions, and public results contain no credential or host path.

The plan is private `0600`, current-owner checked, durable across Edge restart,
five-minute and single-use. It binds the registered workspace, project, target, branch,
local HEAD and fetched remote HEAD. Dirty, detached, ahead, diverged, stale, changed,
malformed, symlinked, expired and replayed state is rejected. Candidate identity is 125
tools at `sha256:9f1ce2ece243c1d5e821adc9b037b21b50941125292485ac43748671d13451c8`.

The immutable Edge release version is still the only external authorization
constraint. No release after `p15.0.12` was inferred or created. Finish all independent
PR/deploy work, then request one exact release number only when real Edge acceptance is
the remaining gate.
