# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p9-brain`
Base: Step 3 `d6654b13214c6c7c170d64a2b905efdd122f1b62` / P8 tag `2e3429c9d6342e8e091cadf65293c5c85b1b3259`

## Current phase

P9 Brain Step 4 is implemented locally and awaiting final cleanup/commit. Brain MCP
tools, runtime configuration, persistent volume, operations, and deployment remain
absent. Production and the current console remain unchanged at P8.

## Step 4 behavior

- pure-Go `modernc.org/sqlite@v1.53.0` with `CGO_ENABLED=0`;
- private mode-0600 disposable cache, FTS5/schema/integrity checks and safe lifecycle;
- bounded transactional full rebuild from Markdown truth;
- BM25 quoted plain-text search, deterministic links/backlinks and broken-link status;
- redacted cache contents with secret canary proven absent;
- incremental updates bound to source writes and rollback after Git failure;
- deletion/rebuild equivalence and concurrent search/write/reindex tests;
- 81.5% package coverage against an 80% gate;
- ordinary suite, coverage gate, vet, build, actionlint, fuzz and Govulncheck green;
- Staticcheck/Race remain blocked locally for known environment reasons and must pass
  in the P9 PR.

## Owner decision preserved

Do not change the deployed console during P9. The future console will use a creative
BIOS-inspired visual language, live durable task/device state, and an OAuth-only
migration, but that work belongs to a separate branch after P9 closure.

## Next safe step

Commit/publish Step 4, then begin Step 5 with failing tests for the isolated Brain
capability and disabled-safe lifecycle. Do not register tools until Step 6 and do not
wire production env/mounts until Step 7. The invariant is no resident service.
