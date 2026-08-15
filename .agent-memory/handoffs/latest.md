# Handoff - durable multiworker task candidate

Updated: 2026-08-14

## Current evidence

- Historical `p15.0.34` and `167 tools` records remain in dated baselines.
- Stock Codex `0.147.0` is deployed on the real Parrot Edge through signed release
  `p15.0.37`; the credential-free GPT Web turn loop completed successfully.
- The current branch adds four public `project_task_*` tools, raising the source catalog
  to 171 tools and
  `sha256:55183a0bc673daed4c364ba0dc4ecb8c976ab32e574c58e6220a7817190fd4fe`.
- Edge-private `project-worktrees.db` owns exact-base worktree identity, generated branch,
  workspace registration, job/lease/fence and explicit cleanup.
- Workqueue schema version 2 owns durable task groups and one to four independently
  leased workers. A server coordinator binds each worker to a signed Edge operation,
  managed worktree, workspace and stock Codex runtime and reconciles after restart.
- Built-in Codex multiagent remains disabled. MCP Devbox supplies the multiagent control
  plane, preventing two writers from sharing a checkout.

## Verified source behavior

- create/replay/conflict, newer-fence claim, stale-fence rejection, status/list and
  clean-only cleanup have focused tests;
- task creation/reuse, worker binding, cancellation, lease-expiry recovery and migration
  have focused tests;
- two distinct worktrees/workspaces/runtimes and terminal reconciliation have server
  integration tests;
- task status omits prompts, paths, leases, fences and credentials;
- worker branches and durable task evidence survive worktree cleanup.

## Remaining closure

Run the full gates, commit, PR, exact-head CI, merge, catalog-aware backend rollout,
official signed Edge release/update, real two-worker GPT Web acceptance, managed restart
and fencing recovery, exclusive cleanup and a Brain closure note. Report source,
production, signed release and installed Edge identities separately.

Do not reset or clean unknown state, expose credentials, enable built-in Codex agents,
add another scheduler/database, or treat a green source commit as installed-device proof.
