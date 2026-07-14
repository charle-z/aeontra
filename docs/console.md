# Console 2.0 authentication and browser boundary

P8.1 evolves the private console into the Neo-BIOS operations firmware to the existing MCP Devbox HTTP
application. React, TypeScript and Vite compile to fixed same-origin assets embedded by Go. Production adds no Node server, database, CDN, or credential: there is no new Coolify application or listener.

## URL

```text
https://<your-mcp-host>/console
```

The console is registered only when the HTTP transport runs. Stdio operation is
unchanged.

## Authentication

### Static-token deployment

Open `/console`, enter the existing `MCP_DEVBOX_TOKEN`, and submit the form over HTTPS.
The token is sent in the request body—not the URL—and is compared in constant time. It
is never placed in the session cookie, response, JSONL observability, or browser
storage.

A successful login creates an opaque random session cookie:

- `HttpOnly`;
- `SameSite=Strict`;
- path `/console`;
- `Secure` for the configured HTTPS public URL and all non-loopback hosts;
- eight-hour expiry;
- in-memory only.

The server stores only a SHA-256 digest of the cookie value. At most 128 sessions are
retained; expired sessions are pruned and the oldest expiry is evicted at the cap.
Logout, expiry, or process restart revokes access.

### OAuth console login

`GET /console/auth/start` creates a bounded digest-only state record and redirects to the existing `/oauth/authorize` page. The owner passphrase is accepted only there. PKCE S256 and exact state are mandatory. The server-side callback consumes the single-use code, creates an opaque console cookie, and redirects to the clean `/console` URL. Access tokens never reach JavaScript.

The static token remains available only as an `Authorization: Bearer` recovery mechanism and through the HTTPS console form. `?key=` query authentication always returns 401.

## Displayed data

The authenticated status endpoint returns exactly:

```json
{
  "status": "ok",
  "version": "0.2.0",
  "protocol_version": "2024-11-05",
  "commit": "<git sha>",
  "tool_count": 67,
  "catalog_hash": "sha256:...",
  "authenticated": true,
  "surface": "presentation-only"
}
```

The React application contains interactive Neo-BIOS screens backed only by strict allowlisted endpoints. Missing data is labeled unavailable rather than fabricated. It also contains architecture, delivery-pipeline, security-boundary,
capability-state, and limitation text.

It does **not** display or accept repository names, branches, paths, source, prompts,
params, results, targets, logs, audit entries, observability history, tokens,
identities, IP addresses, GitHub/Coolify inventories, plans, approvals, deployments,
or tool execution.

## Routes

| Route | Method | Authentication | Purpose |
|---|---|---|---|
| `/console` | GET | Login page or session/direct auth | Render the console shell. |
| `/console/auth/start` | GET | None | Start the console OAuth PKCE flow. |
| `/console/auth/callback` | GET | OAuth state + one-use code | Create an opaque session and redirect cleanly. |
| `/console/login` | POST | Existing static token in form body | Recovery-only opaque session. |
| `/console/logout` | POST | Current cookie, if present | Revoke and clear the session. |
| `/console/status` | GET | Session or existing direct auth | Return the fixed safe status schema. |
| `/console/assets/app.css` | GET | Session or existing direct auth | Embedded Neo-BIOS stylesheet. |
| `/console/assets/app.js` | GET | Session or existing direct auth | Embedded React application bundle. |

Unsupported methods return `405`; login bodies are limited to 4 KiB; malformed or
oversized bodies fail closed.

## Browser security

Console responses set restrictive headers including:

```text
Content-Security-Policy
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
Permissions-Policy: ...=()
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
Origin-Agent-Cluster: ?1
Cache-Control: no-store
```

The authenticated page loads only embedded same-origin CSS and JavaScript. There are
no remote fonts, analytics, third-party scripts, inline scripts, service workers,
WebSockets, `localStorage`, `sessionStorage`, IndexedDB, or JavaScript-readable cookies.

## Installation

No additional install step is required. Build and deploy the normal MCP Devbox image.
The existing HTTP authentication requirement remains mandatory.

Recommended production settings remain:

```text
MCP_DEVBOX_PUBLIC_URL=https://<your-mcp-host>
MCP_DEVBOX_TOKEN=<long-random-secret>
```

OAuth variables may remain enabled. P8 adds no environment variable.

## Updating

1. Publish a tested commit to `main`.
2. Let the existing Coolify application rebuild and restart.
3. Confirm `/healthz`, exact `/version` commit, 67 tools, and catalog hash.
4. Open `/console`, authenticate, and confirm the same commit/tool count/hash.
5. Inspect content-free JSONL events for normalized `route=console` requests only.

Console sessions intentionally disappear during restart. Sign in again after a deploy.

## Rollback

Deploy the prior known-good commit. The console has no database, migration, volume, or
persistent session state to undo. Removing the console package/routes does not alter
MCP tools, OAuth, audit, observability, or repository state.

## Troubleshooting

### Login succeeds but the browser returns to the login page

- Confirm the public console URL is HTTPS.
- Confirm the cookie is present, `HttpOnly`, `SameSite=Strict`, and path `/console`.
- A deployment/restart invalidates all sessions by design.
- Do not disable `Secure` on a public hostname to work around proxy configuration.

### The login form is absent

The process is OAuth-only because no static MCP token is configured. Use an
authenticated OAuth/bearer client or configure the existing static token. P8 adds no
separate console password.

### Assets or `/console/status` return 401

The session is missing, expired, revoked, or from a previous process. Reload
`/console` and authenticate again. Assets and status are intentionally not public.

### Status shows an unexpected commit or catalog

Treat it as a deployment identity problem. Compare `/version`, `system_runtime_info`,
Coolify application status, and the expected Git commit before deploying again. Do not
create a deployment loop.

### Need repository, audit, logs, plans, or tool controls

The console is deliberately unable to provide them. Use the authorized MCP workflows
or private operator evidence path. Do not expand the console into a shadow control
plane.
