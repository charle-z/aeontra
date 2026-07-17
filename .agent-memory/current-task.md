# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac` (PR #14 CodeQL closure).

Current implementation: Step 1 is in progress and package tests are green. The runtime journal now uses `/state/tasks/tasks.db` through a private SQLite store with WAL, busy timeout, cursor pagination, monotonic event IDs, legacy JSON migration, 30-day terminal retention, 10,000-record secondary budget, a 64 MiB page cap, storage health, replay, and explicit derived disconnected state. `/console/tasks` is schema v2 and SSE accepts `Last-Event-ID`.

No merge or deployment is allowed. Catalog must remain 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.
