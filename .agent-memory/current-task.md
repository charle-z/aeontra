# Current task - managed worktrees, durable Codex task groups and real acceptance

Updated: 2026-08-14

## Verified starting point

- The dated operational baseline at signed release `p15.0.34` and the historical
  `167 tools` Codex candidate remain evidence, not current live identity.
- PR #181 productized pinned stock Codex CLI `0.147.0`; PR #182 disabled its opaque
  native web-search path. Production and the real Parrot Edge were accepted at commit
  `2a4c9ac6679743d492de6e5480c32c104762a1f6`, release `p15.0.37`.
- A real credential-free Codex runtime completed two GPT Web model turns and one bounded
  workcell command. GPT Web remains the model; Codex is the local agent harness.
- The current source candidate exposes 171 tools with catalog
  `sha256:55183a0bc673daed4c364ba0dc4ecb8c976ab32e574c58e6220a7817190fd4fe`.

## Active scope

The current branch adds Edge-private exact-base managed Git worktrees, durable worker
jobs with monotonically increasing fences, one registered workspace and independent
stock Codex runtime per goal, restart reconciliation, idempotent cancellation and
clean-only explicit cleanup. A task accepts one to four bounded goals.

Built-in Codex multiagent stays disabled. MCP Devbox itself is the multiagent harness:
it owns task identity, worker leases, worktree branches, workspace registration and
runtime association. Worker branches are retained for explicit normal Git review and
integration; the coordinator never guesses conflict resolution.

## Remaining gates

1. finish canonical docs, catalog identity and full local verification;
2. create one reviewable commit and normal PR;
3. require all exact-head checks green and merge by merge commit;
4. perform the catalog-aware backend rollout and public OAuth/MCP checks;
5. publish one official signed Edge release, install `stable`, and verify doctor;
6. run real two-worker acceptance, cancellation, restart/fence recovery and cleanup;
7. store the final durable Brain handoff.

## Boundaries

- Do not reset, clean, force-push, or discard unknown checkout state.
- Do not infer an installed release from source, CI or backend deployment.
- Do not expose ChatGPT, Codex, OpenAI, GitHub or browser credentials to workers.
- Keep the public MCP container free of host sockets and arbitrary host authority.
- Preserve explicit branches/evidence even after managed worktree cleanup.
