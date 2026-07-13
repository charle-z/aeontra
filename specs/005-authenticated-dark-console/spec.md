# Spec — P8 authenticated dark console

Status: **active** on branch `p8-authenticated-dark-console`.

## Goal

Add a dark, responsive, authenticated presentation console to the existing MCP Devbox
HTTP application without creating another application, control plane, tool endpoint,
or private-data view.

## Authority boundary

The console is presentation-only. It may display only:

- public build version and commit;
- public protocol version;
- public tool count and deterministic catalog hash;
- coarse health state;
- static architecture, delivery stages, security guarantees, and limitations;
- the fact that the viewer has an authenticated console session.

It must never expose or accept:

- MCP tool execution or arbitrary JSON-RPC;
- plan preview/execution or approval actions;
- repository names, branches, paths, source, diffs, prompts, params, results, targets,
  audit entries, observability history, logs, tokens, identities, IPs, or raw errors;
- Coolify/GitHub/application inventories;
- policy mutation, environment mutation, deployment, shell, filesystem, or network
  authority.

## Routes

- `GET /console` — authenticated console page or public minimal login page.
- `POST /console/login` — accepts the existing static MCP bearer token in a form body,
  creates an opaque bounded in-memory console session, and redirects to `/console`.
- `POST /console/logout` — clears the console session.
- `GET /console/status` — authenticated safe JSON containing only public runtime identity
  and fixed presentation metadata.
- `GET /console/assets/app.css` and `GET /console/assets/app.js` — embedded immutable
  assets; authenticated by the console session.

No route is registered when HTTP transport is not running. No new listener is added.

## Authentication

1. Existing valid static bearer/query authentication or OAuth bearer authentication may
   access the console directly.
2. Browser login is available only when the existing static MCP token is configured.
3. Form login compares the token in constant time and never places it in a URL, log,
   response, cookie, or observability event.
4. Successful login creates a cryptographically random opaque session id. Only its
   SHA-256 digest is stored server-side.
5. Session cookies are `HttpOnly`, `SameSite=Strict`, path-scoped to `/console`, and
   `Secure` whenever the configured public URL is HTTPS.
6. Sessions are in-memory, expire after eight hours, are capped at 128, and disappear
   on restart. Logout and expiry revoke access.
7. Authentication failures are generic and preserve constant response shape.

## Browser security

Every console response must set:

- `Content-Security-Policy` with no external origins and no inline script;
- `X-Content-Type-Options: nosniff`;
- `X-Frame-Options: DENY`;
- `Referrer-Policy: no-referrer`;
- `Permissions-Policy` disabling camera, microphone, geolocation, payment, USB, and
  other unnecessary capabilities;
- `Cache-Control: no-store` for HTML/status/login responses;
- asset cache headers only for content-addressed immutable embedded assets.

The UI uses no CDN, remote font, analytics, service worker, localStorage, sessionStorage,
or third-party script.

## Visual requirements

- dark by construction with high contrast and visible keyboard focus;
- responsive from 320 px upward;
- reduced-motion support;
- semantic landmarks and accessible labels;
- status cards, architecture flow, security boundary, capability matrix, and clear
  limitations;
- no fake live activity or fabricated metrics.

## Acceptance

- Unauthenticated users cannot read the console, assets, or status payload.
- A successful form login creates a bounded opaque session; the original token never
  appears in output, logs, events, or cookies.
- All routes reject unsupported methods and oversized/malformed login bodies.
- Status JSON contains only the allowlisted public fields.
- CSP and browser-hardening headers are tested.
- No existing MCP/OAuth route or 62-tool catalog behavior changes.
- Local and GitHub CI/security gates pass.
- Production serves the exact P8 commit and the authenticated console works over HTTPS.

## Non-goals

- No public showcase application yet.
- No Asset Broker, repository browser, audit viewer, log viewer, terminal, plan UI,
  deployment UI, profile manager, or Edge Agent.
- No OAuth protocol change and no new credential.
