# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac`.

Historical deployed foundations retained:
- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain remains the Markdown-truth / SQLite-derived-cache foundation.

Closed commits:
- `c284dcd` — Step 1: durable SQLite Operation Journal and recoverable SSE.
- `e4c674e` — Step 2: durable telemetry windows/lifetime and controller/runtime state.
- `22a9daf` — Step 3: durable digest-only console browser sessions.
- `9cdfe56` — Step 4: shared Neo-BIOS authentication firmware for console and OAuth.
- `aa1c30d` — Step 5: safe Brain node metadata, HMAC IDs and restart-safe identity.

Current verified uncommitted Step 6:
- persistent event log reusing `task_events` with 30-day/20,000-row retention;
- exact event filters and stable opaque cursors;
- recoverable SSE snapshots/replay/gap reset using Last-Event-ID and query fallback;
- JSON migration creates one durable event transactionally and remains idempotent;
- real opaque Project/Edge selectors only;
- combined DB/WAL/log reporting budget, 256 MiB with 75/90% states;
- console data schema v3 and exact TypeScript parser;
- durable Tasks and Events pagination/filters/timestamps;
- EventSource reconnect and complete timer/request cleanup;
- updated baseline/documentation without merge/deploy claims.

Verified so far:
- full `go test ./...` passes;
- frontend TypeScript, Vitest (5 tests) and Vite production build pass;
- migration/restart, SSE replay, quota, storage and selector tests pass.

Catalog remains exactly 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.

Next: run all remaining closure gates, commit Step 6, publish branch, open PR and wait all required checks. No merge or deployment.
