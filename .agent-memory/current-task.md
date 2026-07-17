# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac`.

Completed commits:
- `c284dcd` — Step 1: private SQLite Operation Journal, idempotent legacy migration, bounded retention, cursor pagination, versioned SSE replay and explicit degraded storage state.

Ready to commit as Step 2:
- telemetry lifetime table updated transactionally with hourly/daily buckets;
- exact 24h, 7d, 30d, 90d and lifetime windows;
- current-process requests/tool calls/bytes labeled separately with `estimate, not provider billing`;
- restart test proving process counters reset while durable activity survives;
- read-only controller and runtime models separated from operation history, with no prompts, paths, device keys or IPs.

No merge or deployment. Catalog remains required at 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.
