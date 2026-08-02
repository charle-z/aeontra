# Current task — safe Edge checkout synchronization

## Verified base and production

- Branch: `codex/edge-safe-sync`.
- Base: `origin/main` at Hito 3B merge `1d511cb038141a2a7f4bdf97d2472d4428e1f8d1`.
- PR #126 passed 16/16 exact-head checks and is merged/deployed.
- Backend serves 121 tools at
  `sha256:feca2e4d163cfcff7e08410d5d5b34a52396430d49515c0f601486ffde0b31e2`.
- Front Door transition deployments `ek8coe188tjlsl90vh2cpwk3` and
  `i10q024us3jyv22cw7zl38o1` finished; the old catalog is retired.
- Public OAuth discovery is 200 and unauthenticated `/mcp` is 401 with
  `resource_metadata`.
- Real Edge remains on signed `p15.0.12`; Hito 3A/3B real-device acceptance still
  requires one explicitly numbered signed release and update.

## Candidate scope

- `project_git_status`, `project_git_fetch`, `project_git_fast_forward_preview`, and
  `project_git_fast_forward` operate on the registered Edge checkout only.
- Existing private owner-bound Git credential runner is reused; no new auth path.
- Fetch is exactly `git fetch --no-tags origin`.
- Fast-forward uses a private owner-checked five-minute single-use plan bound to
  project, target, branch, local HEAD and remote HEAD, then only `git merge --ff-only`.
- Dirty, detached, ahead, diverged, stale tracking, changed, malformed, symlinked,
  expired or replayed state fails closed.
- Candidate catalog is 125 tools at
  `sha256:9f1ce2ece243c1d5e821adc9b037b21b50941125292485ac43748671d13451c8`.

## Next exact action

Finish adversarial tests and catalog/documentation reconciliation, run focused and full
gates, review the diff, commit, publish, open a PR, wait exact-head CI, transition the
Front Door, merge/deploy, retire the previous catalog and continue to Hito 4. Do not
infer an immutable release number.
