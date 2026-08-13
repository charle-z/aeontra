# Handoff — reconciliation in progress; Codex and multiagent not started

Updated: 2026-08-12

## Current evidence

- Source/production commit:
  `f8d0a38af06527dcf59763c793bee81aca9dd044`.
- Production identity: protocol `2024-11-05`, 166 tools, catalog
  `sha256:088a0bacfb5a631bb8b4fd45185ec3f997648992ab9810f7a401d0d4d8eeefe5`.
- Front Door: healthy at
  `489a64f40cbbde014986ff130662a485f9513d6c`.
- Real Edge: signed `p15.0.34` at
  `04c544b776ffca2071cb5b5a9951b8b32f423a36`; doctor ready, bundle/layout/identity
  valid, service active, one process, lock held, managed coherence, rootless Podman and
  empty journal.
- Browser harness: merged through PR #154 and present in the installed release. PR #153
  is closed and must not be resumed.
- Main workflows are green; there are no open PRs/issues.

See `docs/baselines/2026-08-12-operational-reconciliation.md` for the complete dated
snapshot and limitations.

## Work in progress

Branch `codex/reconcile-roadmap` updates only documentation truth and its regression
test. It does not change the catalog, runtime, Edge bundle, production or external
resources.

After exact-head merge:

1. Obtain/check durable checkpoints for the operator-reported active mathematical VPS
   work and Edge public-OSS work.
2. Audit operations and remote branches. Remove only exact terminal/obsolete resources
   whose ownership and replacement are proven.
3. Publish the next signed stabilization release from then-current green main and
   install it once on the real Edge.
4. Create a separate Codex spike branch. Pin an official artifact, run it in a
   disposable toolbox, and prove scripted provider/App Server behavior before changing
   the signed bundle.
5. If stock Codex can be paused/resumed through the durable model-turn bridge, build the
   signed Edge adapter and retain OpenCode as rollback. Otherwise record the exact
   missing extension before considering a minimal fork.
6. Implement worktrees and one-writer ownership, then connect the existing P16
   workqueue to the durable task graph and multiagent execution.

## Do not infer

- A green source head is not an installed Edge release.
- The unavailable VPS SSH session does not prove that Codex is installed or absent on
  that host.
- The P16 store/fairness code does not mean public multiagent execution exists.
- ChatGPT browser automation is not required for durable continuation and must not be
  coupled to consequential replay.

No restart, Edge update, cleanup, deployment, release, Codex installation or
multiagent execution occurred during this reconciliation.
