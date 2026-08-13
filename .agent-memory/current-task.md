# Current task — reconcile state and prove a Codex harness

Updated: 2026-08-12

## Verified starting point

- Source and production serve
  `f8d0a38af06527dcf59763c793bee81aca9dd044`.
- Production reports protocol `2024-11-05`, 166 tools and catalog
  `sha256:088a0bacfb5a631bb8b4fd45185ec3f997648992ab9810f7a401d0d4d8eeefe5`.
- Front Door is healthy at
  `489a64f40cbbde014986ff130662a485f9513d6c`.
- The real `parrot-trusted-linux` Edge is ready on signed release `p15.0.34`,
  commit `04c544b776ffca2071cb5b5a9951b8b32f423a36`, with one managed process,
  held lock, valid rootless Podman and an empty journal.
- PR #154, not the closed integration PR #153, delivered the general managed browser
  harness and is included in the installed Edge release.
- Main exact-head workflows are green. GitHub reported no open PR and no open issue.

The dated evidence and explicit non-actions are in
`docs/baselines/2026-08-12-operational-reconciliation.md`.

## Active scope

Phase 0 reconciles documentation and operational truth. Phase 1 then evaluates stock
Codex CLI/App Server as an optional Edge harness whose model requests are satisfied by
the existing durable GPT Web model-turn protocol.

The preferred order is:

1. merge the reconciliation through normal exact-head CI;
2. inventory active operations and classify obsolete remote branches/resources;
3. wait for durable checkpoints from operator-reported active VPS research and Edge OSS
   work before any Edge restart or update;
4. publish the next stabilization release from a green main and update the Edge once;
5. run a disposable, pinned Codex compatibility spike without an OpenAI API key;
6. keep OpenCode as rollback until one signed Codex release passes real-device
   acceptance;
7. implement managed worktrees before writing multiagent execution;
8. connect multiagent task identities to the existing P16 workqueue instead of adding
   another scheduler or database.

## Boundaries

- Do not reset, clean, force-push or discard unknown checkout state.
- Do not restart/update the Edge, stop workloads or remove toolboxes while active work
  lacks a checkpoint.
- Do not install Codex into the public MCP container or give a VPS worker a rootful
  Docker socket.
- Do not expose GitHub credentials to Codex, its workspace or its model turns; retain
  the existing private brokers.
- Do not fork Codex until a scripted stock-provider/App Server spike records a concrete
  incompatibility.
- Do not begin writing multiagent execution before deterministic worktree ownership and
  cleanup exist.

## Current branch

`codex/reconcile-roadmap` is based exactly on the verified main head. It contains only
the dated reconciliation, corrected roadmap/current handoff and their documentation
contract. Codex integration belongs in a separate branch after this change merges.
