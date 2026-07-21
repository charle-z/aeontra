# Console durable live state, authentication and browser boundary

The console is an authenticated, presentation-only surface embedded in the existing Go HTTP application. React, TypeScript and Vite compile to fixed same-origin assets embedded by Go. Production adds no Node server, database server, queue, terminal, browser automation or second control plane.

## URL and visual contract

```text
https://<your-mcp-host>/console
```

The authenticated console and `/oauth/authorize` use the same Neo-BIOS/VGA firmware stylesheet. The surface uses monospaced text, square geometry, tabbed screens, keyboard/mouse/touch navigation, accessible help/tooltips and `prefers-reduced-motion`. There are no gradients, external fonts, remote scripts or inline script/style blocks.

## Authentication and durable sessions

OAuth remains the normal path. `GET /console/auth/start` creates bounded one-use state and PKCE S256 material, then redirects to `/oauth/authorize`. The owner passphrase is submitted only to that server endpoint. `/console/auth/callback` validates and consumes the authorization flow, creates an opaque browser session and redirects to the clean `/console` URL. Access tokens never reach JavaScript.

Recovery bearer authentication remains separate from OAuth and is accepted only through the `Authorization` header or the HTTPS form body. Query-string credentials remain rejected.

Browser sessions persist in `/state/console/sessions.db`. The database stores only SHA-256 digests, creation/expiry timestamps, revocation state and version. The raw cookie and bearer token are never persisted. The production default uses a 60-year practical persistence horizon instead of a short inactivity timeout, so OAuth/recovery sessions survive process replacement without routine token entry. Sessions still end on logout, explicit revocation, browser cookie deletion, loss of the persistent `/state` volume, or oldest-session eviction after the bounded 128-session cap. Session files are private and SQLite corruption or unsafe permissions fail closed. A deployment from the old memory-only implementation requires one final login because there is no safe material to migrate.

## Safe routes

| Route | Method | Authentication | Purpose |
|---|---|---|---|
| `/console` | GET | Login or opaque session/direct auth | Render the firmware shell. |
| `/auth/assets/firmware.css` | GET | None | Shared static authentication firmware CSS. |
| `/console/auth/start` | GET | None | Start console OAuth state/PKCE flow. |
| `/console/auth/callback` | GET | Exact state and one-use code | Create durable session and redirect cleanly. |
| `/console/login` | POST | Recovery token in body | Create durable recovery session. |
| `/console/logout` | POST | Current session | Durably revoke and clear session. |
| `/console/status` | GET | Session or direct auth | Exact runtime identity schema. |
| `/console/data` | GET | Session or direct auth | Exact safe aggregate schema version 3. |
| `/console/tasks` | GET | Session or direct auth | Durable task page with `limit` and opaque `cursor`. |
| `/console/event-log` | GET | Session or direct auth | Persistent event page with exact filters and cursor. |
| `/console/events` | GET | Session or direct auth | Recoverable SSE stream. |
| `/console/assets/app.css` | GET | Session or direct auth | Embedded console CSS. |
| `/console/assets/app.js` | GET | Session or direct auth | Embedded React bundle. |

Unknown query keys, duplicate query values, invalid enums, malformed cursors and unsupported methods fail closed. Dynamic responses are `no-store` and carry strict frame, content-type, referrer, permissions and same-origin headers.

## Durable Operation Journal and Event Log

`MCP_DEVBOX_TASK_ROOT=/state/tasks` contains `tasks.db`, not a bounded set of browser JSON files. The journal stores only opaque task ID and sequence, closed controller/operation/state values, fixed safe summary, precise UTC timestamps, version and terminal state. Prompts, tool parameters, results, repository names, paths, tokens, IPs and private identities are never journaled.

Task pages use stable opaque cursors and support more than 500 operations without the former 256-record ceiling. Terminal tasks have 30-day retention and a 10,000-task cap. The same SQLite database contains `task_events`, which is the sole truth for both `/console/event-log` and `/console/events`. Events retain 30 days and at most 20,000 rows.

Legacy per-task JSON files are imported transactionally and idempotently. A successfully inserted legacy task receives exactly one durable migration event; reopening the database does not duplicate either row. Migrated JSON files are archived only after the database transaction commits.

`/console/event-log` accepts only `limit`, `cursor`, `controller`, `state`, `operation` and `event_type`. Filters are exact, not substring searches. `/console/tasks` accepts only `limit` and `cursor`.

## Recoverable SSE

The browser receives `snapshot`, `event_snapshot`, persisted `journal` events and a `stream` live marker. The server sends `retry: 2000`. Reconnection uses `Last-Event-ID`; the browser also supplies the same value through `last_event_id` because a newly constructed `EventSource` cannot set custom headers. Conflicting header/query IDs fail closed. A retained range is replayed without duplicates. A gap or future ID produces fresh task/event snapshots before the live marker.

React exposes `connecting`, `live`, `reconnecting` and `offline`, retries with bounded exponential backoff, merges live events by durable `event_id`, preserves events that arrive before the initial JSON page, and closes EventSource, reconnect timers, refresh timers and filter requests on cleanup.

## Real data and opaque selectors

The UI does not invent projects, devices, agents or measurements.

- **System:** exact runtime identity plus real container CPU, RAM, disk, load and a combined storage budget.
- **Agents:** controllers and model runtimes are separate entities. Tool calls remain operations, never agents.
- **Tasks:** durable pages, exact filters, versions and precise timestamps.
- **Brain:** aggregate index state and a bounded real link graph.
- **Graph:** stable HMAC node IDs plus explicit redacted title, `console_summary`, trust and degree. No slug, body, private provenance or path.
- **Edge:** active durable Edge devices only. Raw device ID, name, key and network details remain private.
- **Projects:** one option per real configured policy root. Paths and repository names remain private.
- **Events:** server-persisted journal history, not browser-generated notices.

Project selector IDs are stable domain-separated SHA-256 derivations over the real root ordinal and use generic labels such as `Configured project 1`. Edge selector IDs are stable derivations over the server-generated random device ID and use generic labels such as `Paired Edge 1`. These selectors currently scope the presentation only; they grant no additional authority.

## Brain console metadata

Brain node identity is keyed by `/state/brain/console-node.key`. The directory is exactly `0700`, the file exactly `0600`, symlinks and unsafe existing permissions are rejected, and creation uses an atomic no-overwrite publication. The key is not regenerated when present and is never returned, logged or included in errors.

Node IDs are domain-separated HMAC-SHA-256 outputs. They are stable across restart and cannot be reversed to a slug. `console_summary` is optional explicit frontmatter metadata, single-line and at most 160 bytes. During reindex, the derived `console_metadata` table is replaced in the same transaction as `notes`; incremental updates use the same upsert. Title and summary are secret-scanned and use fixed safe fallbacks. The body is never used to synthesize a summary.

## Storage budgets

The journal SQLite database enforces a 64 MiB page cap. The console also reports a combined private-state budget for SQLite databases, WAL/SHM files and `.log`/`.jsonl` files. Paths and filenames never leave the server.

The combined reporting limit is 256 MiB: below 75% is `healthy`, from 75% is `nearing_limit`, and from 90% is `degraded`. The scanner rejects symlinks, non-regular files and more than 4,096 entries. This is an observability budget, not permission to delete arbitrary state.

## Browser security

Only same-origin embedded assets are loaded. CSP permits same-origin script/connect sources for REST and SSE and excludes `unsafe-inline`. There are no WebSockets, remote fonts, analytics, third-party scripts, inline handlers, `dangerouslySetInnerHTML`, `innerHTML`, eval, service workers, local/session storage, IndexedDB or JavaScript-readable cookies.

The console remains presentation-only. F9/F10 do not approve or execute anything. MCP single-use plans remain the only authority path for consequential actions.

## Production configuration

```text
MCP_DEVBOX_PUBLIC_URL=https://<your-mcp-host>
MCP_DEVBOX_OAUTH_PASSPHRASE=<configured-owner-passphrase>
MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json
MCP_DEVBOX_OAUTH_REFRESH_STORE=/state/oauth-refresh.json
MCP_DEVBOX_TASK_ROOT=/state/tasks
MCP_DEVBOX_BRAIN_ROOT=/brain
MCP_DEVBOX_STATE_ROOT=/state
MCP_DEVBOX_TOKEN=<optional-recovery-value>
```

Reuse the existing private `/state` and `/brain` persistent mounts. Do not create another application.

## Upgrade and rollback

Before rollout, verify the exact catalog:

```text
86
sha256:ea9cc3749c68fcc12b608efbddc259b01eb7868c98bbc1ab35c75f456e118a98
```

Upgrade is additive and idempotent: legacy task JSON is imported into `/state/tasks/tasks.db`; durable sessions begin in `/state/console/sessions.db`; `/state/brain/console-node.key` is created once; and Brain reindex creates/refreshes `console_metadata` without changing Markdown truth.

Rollback by deploying the previous known-good binary while keeping `/state` and `/brain` intact. Never delete the databases or Brain key to make an old binary start. Older binaries ignore new tables/files they do not use. If the console reports unavailable or degraded state, inspect private mounts and permissions; do not fabricate fallback values or expose private paths.

## Historical P8/P8.1 compatibility markers

P8 originally shipped an **eight-hour expiry** and **128 sessions** maximum backed by durable SQLite instead of process memory. The current production default supersedes only the short TTL with a **60-year practical persistence horizon**; the bounded session cap, explicit logout/revocation, **HttpOnly**, `SameSite=Strict`, Secure cookies and **Content-Security-Policy** remain. This milestone remains inside the existing application: there is **no new Coolify application**. Edge renders **Not paired** when the real registry contains no active device.

## Troubleshooting

If durable sessions fail after restart, verify `/state/console` is a real 0700 directory and `sessions.db` is a regular 0600 file; do not chmod unsafe existing paths automatically. If SSE remains reconnecting, compare the displayed Last-Event-ID with retained journal bounds and query `/console/event-log`; a retention gap should yield fresh snapshots. If Project or Edge selectors are empty, verify real policy roots or active durable Edge devices rather than adding placeholder options. If storage is unavailable, inspect the private state tree for symlinks, non-regular files or excessive entries. Keep the console presentation-only and never expose private paths while diagnosing.
