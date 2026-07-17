# Latest handoff — console durable live state

Date: 2026-07-17
Branch: `console-durable-live-state`
HEAD before final closure: `0838476dd5d37e834b61544ddfd823f6d1e852b4`


Historical deployed foundations:
- P8.1 is closed, deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- Its historical catalog had 67 tools and Edge state `not_paired`.
- P9 Brain is deployed and preserved as the Markdown-truth / SQLite-derived-cache foundation.

Closed work:
- Steps 1–5 are committed as `c284dcd`, `e4c674e`, `22a9daf`, `9cdfe56` and `aa1c30d`.
- Step 6 is committed as `225b6e1` and includes persistent Events, recoverable SSE, server-side filters/cursors, real opaque Project/Edge scopes, the 256 MiB combined state budget and the React live-state implementation.
- `0838476` merged main through the verified regex-alert closure.

Pending tree contains only Step 7 closure updates: current catalog identity, documentation consistency, one Brain runtime catalog assertion and frontend test synchronization. It must be preserved and committed before merging latest main.

Latest production/main now includes the GitHub Checks→Actions fallback and the required-status-checks 403 handling through merge `77a93ad110af287b402c271703cd9ae1502a2582`. Integrate it with a normal merge; do not rebase or force push.

Catalog invariant: exactly 85 tools with hash `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Required next steps:
1. validate and commit `Step 7: close console durable live state`;
2. fetch and merge `origin/main` normally;
3. run the full local gates and exact catalog checks;
4. publish only `console-durable-live-state`, open its PR, wait for all gates, merge by merge commit and allow only the automatic Coolify deployment.
