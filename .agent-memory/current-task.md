# Current task — bounded Front Door catalog rollout recovery

## Incident

- Backend deployment `lq9921aa3qhb858wmelwmwwk` is terminal `finished` at
  `ecc1bef21aab0a144a9129a9d9907313661658c5`.
- The backend is healthy and reports protocol `2024-11-05`, 115 tools and catalog
  `sha256:d1dab9c0d265284dc66d8c07a0c78b59aa1bd5d89d256255ab5862268e858bfb`.
- The existing Front Door `o338wpoy1254d83ud2y8p1v8` runs
  `ced84aade8a691e487b4ca7448a87df42c9da0cb` but admits only the former catalog
  `sha256:327a5ac4830172c9c64545c9b7d121487c773aed255f7c64e732606b491eaf99`.
- Its runtime reports `backend_incompatible`; the official connector origin returns
  503 for MCP and OAuth because all non-diagnostic routes were behind catalog admission.

## Active correction

- Branch: `codex/front-door-catalog-transition`, based on `origin/main` at
  `ecc1bef21aab0a144a9129a9d9907313661658c5`.
- Keep one required primary catalog plus at most one distinct exact transition hash.
- The managed Front Door plan authenticates the current primary environment value,
  rejects a third catalog, seals the transition state into the single-use plan and
  removes the old transition on a later same-primary reconciliation.
- OAuth discovery, authorization, token and RFC 7591 routes proxy to the fixed backend
  independently from MCP catalog admission; `/mcp` and SSE remain fail-closed.
- No public tool schema or description changed, preserving the 115-tool catalog hash.

## Verification so far

- Focused Front Door, tools, command and docs tests pass.
- Full suite passed all affected packages and the exact 115-tool/hash assertions.
- Two provider fixtures timed out only under full concurrent load and passed isolated.
- `packaging/builder` mode assertion is not reproducible on this Windows/DrvFS worktree
  (`0777` observed instead of Linux `0755`); Linux CI remains authoritative.
- `go vet ./...`, `go build ./...` and `git diff --check` pass in WSL.

## Required continuation

1. Review the complete diff and commit the focused correction.
2. Push and open a non-draft PR to `main`; require exact-head CI green and merge commit.
3. Advance `front-door-stable` through a separate reviewed PR containing the same
   Front Door correction.
4. Configure exactly the old and new hashes on the existing Front Door and deploy that
   application once, normal/cached.
5. Verify OAuth discovery, real DCR, unauthenticated `/mcp`, MCP initialize/tools/list,
   `project_exec`, SSE/session continuity and exact backend/Front Door identity.
6. In a later managed reconciliation, make the new hash primary and delete the old
   transition; deploy once and prove the old catalog is rejected.
7. Record production evidence in a dated baseline and Brain before Edge release work.

Do not publish `p15.0.12`, update Edge, start background execution or dispatch a
cutover until the official connector recovery and old-catalog retirement are proven.
