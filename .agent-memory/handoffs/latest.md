# Handoff - signed Codex productization in verification

Updated: 2026-08-14

## Current evidence

- The dated baseline records the real Edge on signed `p15.0.34` and the previous
  production catalog at 166 tools. It remains historical evidence, not a claim about
  current live state.
- The unmerged candidate on `codex/codex-signed-harness` exposes 167 tools with catalog
  `sha256:8ce8ca2897c7550546ba1277bbe590670c0d4d6648959b7362bc0bd9114cb523`.
- Stock Codex CLI `0.147.0` is pinned by official tag, source commit, release asset,
  archive digest, and binary digest.
- The launcher uses the accepted strict loopback Responses adapter, an isolated
  `CODEX_HOME`, no OpenAI authentication, and the existing Bubblewrap workcell.
- Codex built-in multiagent is disabled. The existing P16 workqueue is not yet connected
  to managed worktrees or multiagent runtimes.

## Rollout contract

The old installed updater is manifest-v3-only. Do not dispatch a v4 bundle directly.
After exact-head CI and merge:

1. publish and install one signed `bridge-v3` release, which keeps the OpenCode unit;
2. verify updater, bundle, service, process, lock, and journal;
3. publish and install one signed `codex-v4` release;
4. verify Codex binary/pin, model adapter, first model turn, cancellation, restart, and
   the explicit OpenCode rollback path.

No release or real Edge acceptance has occurred for this candidate yet.

## Continuation

Once the signed Codex release is accepted, implement managed worktrees and deterministic
one-writer fencing before enabling any multiagent writer. Then connect task identities to
the existing P16 queue, prove bounded read-only fan-out, add isolated writers and
deterministic integration, and test cancellation/restart/fairness on the real Edge.

Do not reset or clean unknown state, expose credentials, add another scheduler/database,
or interpret a green source commit as an installed release.
