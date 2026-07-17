# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac`.

Completed commits:
- `c284dcd` — Step 1: private SQLite Operation Journal, idempotent legacy migration, bounded retention, cursor pagination, versioned SSE replay and explicit degraded storage state.
- `e4c674e` — Step 2: transactional lifetime telemetry, exact durable windows, current-process labeling, and separate controller/runtime state.

Step 3 validated and ready to commit:
- production console sessions persist at `/state/console/sessions.db`;
- only SHA-256 digests plus created/expiry/revocation/version metadata are stored;
- OAuth and Bearer-created browser sessions survive handler restart;
- logout revocation and expiration survive restart;
- max sessions, concurrency, private permissions, symlink rejection, corruption fail-closed and a small SQLite budget are tested;
- the first deployment still requires a one-time login because historical in-memory sessions cannot be migrated.

No merge or deployment. Catalog remains required at 78 tools with hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.
