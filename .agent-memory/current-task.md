# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac`.

Historical deployed foundations retained:
- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed and remains the Markdown-truth / SQLite-derived-cache foundation.

Completed commits on this branch:
- `c284dcd` — Step 1: durable SQLite Operation Journal and recoverable SSE.
- `e4c674e` — Step 2: durable telemetry windows/lifetime and controller/runtime state.
- `22a9daf` — Step 3: durable digest-only console browser sessions.
- `9cdfe56` — Step 4: shared Neo-BIOS authentication firmware for console and OAuth.

Step 5 validated and ready to commit:
- persistent private Brain console identity at `<state-root>/brain/console-node.key`;
- exact directory/file permissions 0700/0600 with fail-closed symlink and permission checks;
- atomic no-overwrite key publication and stable domain-separated HMAC node IDs;
- explicit `console_summary` metadata bounded to 160 bytes;
- transactional and idempotent `console_metadata` derivation during reindex and incremental updates;
- safe title/summary fallbacks after secret redaction;
- no slug, body, private provenance or path in the console graph;
- React graph shows safe title, summary, trust and degree with keyboard-accessible tooltips;
- Go packages, runtime restart tests, exact catalog identity, TypeScript check, Vitest and Vite build pass.

Catalog remains exactly 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.

No merge or deployment.
