# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: Step 6 `022c5fadd820e3249b25da62b387147493010105` / P8 tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 7 is implemented locally and awaiting final gates/commit. The local
candidate has 67 tools and complete runtime/operations wiring; production remains P8
with 62 and the current console is unchanged.

## Step 7 evidence

- optional absolute `MCP_DEVBOX_BRAIN_ROOT` with disabled-safe unset behavior;
- configured startup creates/verifies private layout, local no-remote Git and FTS5,
  performs strict reindex and fails closed on unsafe/malformed state;
- Brain root overlap with repository roots is rejected and never reflected in errors;
- Docker image prepares a dedicated `/brain` persistent volume for non-root UID 10001
  and reproducibly copies `go.sum`;
- operational runbook covers curation, backup, restore, update, rollback and failures;
- read-only `cmd/brain-smoke` verifies production without printing credentials or note
  content;
- coverage: app 71.3%, smoke 76.6%, Brain 81.2%, tools 73.9%, server 82.6%, catalog
  85.6%.

## Owner decision preserved

Do not change the deployed console during P9. BIOS-inspired UI, live task/device state
and OAuth-only migration belong to a separate post-P9 branch.

## Next safe step

Commit/publish Step 7 after full local gates. Then open the P9 PR and require remote
Race, Staticcheck, CodeQL, Dependency Review, Docker/SBOM/Grype before any merge,
Coolify env/volume mutation or deploy; no resident service.
