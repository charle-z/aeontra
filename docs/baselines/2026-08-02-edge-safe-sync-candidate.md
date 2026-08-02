# Safe Edge checkout synchronization candidate — 2026-08-02

Status: candidate; PR, merge, signed release and real Edge acceptance remain pending.

- Base: `origin/main` at `1d511cb038141a2a7f4bdf97d2472d4428e1f8d1`.
- Candidate catalog: 125 tools,
  `sha256:9f1ce2ece243c1d5e821adc9b037b21b50941125292485ac43748671d13451c8`.
- New public surface: `project_git_status`, `project_git_fetch`,
  `project_git_fast_forward_preview`, and `project_git_fast_forward`.
- Git authority: the existing private Edge credential runner; no new authentication.
- Fetch: `git fetch --no-tags origin` with an Edge-constructed branch refspec, no
  caller URL/refspec/tag/force input.
- Write: private `0600`, owner-checked, five-minute, exact and single-use plan followed
  only by `git merge --ff-only` of the bound remote commit.
- Fail-closed cases: dirty tree, detached HEAD, local commits, divergence, stale tracking
  ref, owner/remote mismatch, malformed or symlinked plan, expiry, replay and state change.
- Output: project/repository identity plus branch, HEAD, remote HEAD, ahead/behind,
  clean/dirty/detached/diverged/fetched and plan metadata; never local path, URL or token.

Focused Linux verification covers the operation contract, fixed Git command sequence,
relation parsing, exact plan consume, replay rejection and dirty-tree rejection. Full
CI remains authoritative for race, Bubblewrap, rootless, packaging and OpenCode
compatibility.
