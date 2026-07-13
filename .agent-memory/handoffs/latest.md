# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: Step 2 `fd810aad507ef118570a5097b40945f7138a57df` / P8 tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 3 is implemented locally and awaiting final gates/commit. SQLite,
MCP tools, runtime configuration, volume, and deployment remain absent.

## Step 3 security behavior

- private local repository with exact cache ignore and no remote;
- fixed absolute Git binary, stripped environment, no shell, no prompt, no hooks,
  no filters, no global/system config, denied protocols, bounded output;
- local plumbing-only commits and compare-and-swap ref updates;
- atomic mode-0600 working writes, one commit per success, same-author updates;
- serialized concurrency and clean linear history;
- rollback on commit/ref failure and independent verification of ambiguous ref results;
- symlink/metadata-swap/unsafe ignore/remote rejection;
- generic errors without paths, Git stderr, slugs, or note contents;
- 80.5% package coverage with an 80% gate.

## Next safe step

Commit/publish Step 3 after full gates. Step 4 starts with failing FTS5/index tests and
only then adds `modernc.org/sqlite@v1.53.0`. Do not register tools or wire runtime env
before their planned steps. The invariant is no resident service.
