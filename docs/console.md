# Console 2.0 authentication, data and browser boundary

P8.1 evolves the embedded P8 console into the Neo-BIOS operations firmware inside the
existing MCP Devbox Go HTTP application. React, TypeScript and Vite compile to fixed
same-origin assets embedded by Go. Production adds no Node server, listener, database server, queue, free terminal, Edge device or agent, and there is no new Coolify application.

## URL and visual contract

```text
https://<your-mcp-host>/console
```

The design source is `docs/console-2.0/design-neo-bios.md`; the HTML mockup is a
visual reference only. Production uses one blue VGA BIOS theme (`#0000A8`), the
16-color VGA palette, monospaced text, square geometry, tabbed firmware screens,
Item Specific Help, function-key hints, keyboard/mouse/touch navigation and
`prefers-reduced-motion`.

## Authentication

### OAuth console login

`GET /console/auth/start` creates bounded one-use state and PKCE S256 material,
stores only the SHA-256 state digest and redirects to the existing
`/oauth/authorize` page. The owner passphrase is accepted only there. The
server-side `/console/auth/callback` validates exact state, verifier, client,
callback, scope, audience and authorization code, consumes the code once, creates an
opaque session and redirects to the clean `/console` URL. Access tokens never
reach JavaScript.

The cookie is `Secure`, `HttpOnly`, `SameSite=Strict`, scoped to
`/console` and represented server-side only by a SHA-256 digest. Browser storage
is not used. Sessions have an eight-hour expiry, are capped at 128 sessions, and are revoked on logout or process restart.

### Recovery bearer

`MCP_DEVBOX_TOKEN` is optional when OAuth is configured. It remains available
only as an `Authorization: Bearer` recovery header and through the HTTPS console
form body. Query-string authentication was permanently removed: every
`?key=<value>` returns HTTP 401, including the correct token.

Persist `MCP_DEVBOX_OAUTH_CLIENT_STORE` and
`MCP_DEVBOX_OAUTH_REFRESH_STORE` under `/state` so ChatGPT OAuth survives
a container replacement.

## Safe routes

| Route | Method | Authentication | Purpose |
|---|---|---|---|
| `/console` | GET | Login or opaque session/direct auth | Render shell/bootstrap. |
| `/console/auth/start` | GET | None | Start console OAuth state/PKCE flow. |
| `/console/auth/callback` | GET | Exact state and one-use code | Create session and redirect cleanly. |
| `/console/login` | POST | Recovery token in body | Create recovery session. |
| `/console/logout` | POST | Current session | Revoke and clear session. |
| `/console/status` | GET | Session or direct auth | Exact runtime identity schema. |
| `/console/data` | GET | Session or direct auth | Exact safe aggregate data schema. |
| `/console/tasks` | GET | Session or direct auth | Durable content-free task snapshot. |
| `/console/events` | GET | Session or direct auth | Server-Sent Events for task state. |
| `/console/assets/app.css` | GET | Session or direct auth | Embedded Neo-BIOS CSS. |
| `/console/assets/app.js` | GET | Session or direct auth | Embedded React bundle. |

Unsupported methods return 405. Login bodies are limited to 4 KiB. Dynamic responses
are no-store and use hardened cross-origin and frame headers.

## Displayed real data

The UI never invents devices or metrics. It renders only exact allowlisted fields:

- System: runtime identity plus container CPU, RAM, disk and load; unavailable probes
  are labeled unavailable.
- Agents: authenticated console state, latest generic controller activity and MCP
  payload bytes with declared `bytes / 4 (estimate)` token approximation.
- Tasks: bounded durable state under `MCP_DEVBOX_TASK_ROOT` (`/state/tasks`
in the image), heartbeat and controller. Only public operation names and fixed
summaries are stored.
- Brain: readiness, schema, aggregate counts and timestamp; no note content.
- Graph: real bounded Brain links with opaque ordinal IDs, trust and degree. No slug,
  title, body, author or provenance.
- Observability: aggregate normalized route requests, 4xx, 5xx and P95 only; no raw
  log event or request content.
- Security: OAuth/bearer/query/cookie/free-shell posture.
- Events: safe browser refresh and task transition notices only.
- Edge: exactly `Not paired` until P11.

Task states are exactly requested, planned, awaiting_approval, executing, observing,
validating, completed, failed, cancelled and disconnected. If a controller heartbeat
expires, the presentation becomes disconnected. The console never claims autonomous
work and never exposes private model reasoning.

## Browser security

The authenticated page loads only same-origin embedded assets. The `Content-Security-Policy` includes same-origin
script and connect sources for REST and SSE. No WebSockets are used. There are no
remote fonts, analytics, third-party scripts, inline scripts, inline handlers,
`dangerouslySetInnerHTML`, `innerHTML`, eval, service workers,
`localStorage`, `sessionStorage`, IndexedDB or JavaScript-readable cookies.

## Installation and update

Build and deploy the normal image. Recommended production configuration:

```text
MCP_DEVBOX_PUBLIC_URL=https://<your-mcp-host>
MCP_DEVBOX_OAUTH_PASSPHRASE=<configured-owner-passphrase>
MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json
MCP_DEVBOX_OAUTH_REFRESH_STORE=/state/oauth-refresh.json
MCP_DEVBOX_TASK_ROOT=/state/tasks
MCP_DEVBOX_TOKEN=<optional-recovery-value>
```

The Docker image reserves `/state` and creates private `/state/tasks` for
UID/GID 10001. Reuse the existing persistent `/state` storage; do not create a
new application.

Update only through a tested `main` commit and the existing Coolify app. During
container replacement an existing MCP connection may disconnect briefly; wait for
health and reconnect without creating a duplicate deployment. Then verify exact
commit, OAuth login, strict cookie, query-key 401, recovery bearer, data/task/SSE
schemas, 67 tools, P9 catalog hash and both catalog/Brain smokes.

## Rollback and Troubleshooting

Rollback by deploying the previous known-good `p9` commit/tag. Keep
`/state` and `/brain` intact; old binaries ignore `/state/tasks`.
Task records are closed content-free JSON and require no database migration.

If OAuth login returns to the login page, verify HTTPS, callback/public URL and the
strict cookie, then retry because process restart invalidates console sessions. If SSE
disconnects, polling `/console/tasks` remains the durable fallback. If data is
unavailable, inspect runtime health and mounts; do not fabricate fallback values. If
commit/catalog differ, stop and resolve deployment identity before retrying. Repository
content, raw logs, prompts, params, results, paths, tokens, identities and tool control
remain intentionally outside the console.
