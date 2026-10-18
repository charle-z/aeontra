# Plan — P8 authenticated dark console

Status: **complete**.

1. **Threat model and route contract** — define presentation-only data, authentication,
   session lifecycle, browser headers, and explicit forbidden authority.
2. **Opaque session store** — bounded, expiring, digest-only, concurrency-safe sessions
   with cryptographic ids and deterministic tests.
3. **Embedded assets** — dependency-free semantic HTML, dark CSS, and small same-origin
   JavaScript for safe status refresh and logout.
4. **HTTP integration** — login, logout, console, status, and asset routes inside the
   existing HTTP mux; no new listener or application.
5. **Browser hardening** — CSP, frame/referrer/content-type/permissions/cache headers,
   strict methods and body limits.
6. **Adversarial tests** — tokens, prompts, paths, targets, query secrets, session ids,
   malformed forms, method confusion, and unauthorized asset/status access.
7. **Operations** — installation, secure cookie/public URL behavior, login/logout,
   update, rollback, troubleshooting, and limitations.
8. **Closure** — complete: PR and post-merge gates, dated baseline, PR merge,
   automatic deployment, authenticated production smoke, and safe console log evidence.

## Design rules

- Reuse existing HTTP authentication helpers; do not modify OAuth protocol behavior.
- Store only session digests and expiry, never the MCP token or raw session id.
- Generate all displayed runtime values from the existing safe `RuntimeInfo` contract.
- No generic template data map or endpoint that can grow into private state.
- No frontend package manager, build system, remote dependency, or generated bundle.
- Console code must remain removable without affecting MCP functionality.
