# Threat model — P8 authenticated dark console

## Assets

- Existing MCP bearer token and OAuth access tokens.
- Opaque console session ids.
- Private repositories, paths, source, prompts, params, results, targets, audit entries,
  observability history, deployment metadata, and infrastructure credentials.
- Browser integrity: same-origin assets, authenticated status, and logout behavior.

## Trust boundaries

1. An unauthenticated browser can reach login routes over the public reverse proxy.
2. The form token is untrusted input until constant-time validation succeeds.
3. Console session cookies cross the browser/server boundary on `/console` only.
4. Embedded HTML/CSS/JS are versioned application assets and use no external origin.
5. Runtime identity is sourced only from the existing public `RuntimeInfo` contract.
6. MCP tools, OAuth endpoints, audit, observability, GitHub, and Coolify stay outside
   the console package.

## Threats and controls

| Threat | Control |
|---|---|
| Bearer token leaks through URL/history/referrer | Form body login only; no token query generated; `Referrer-Policy: no-referrer`; token never persisted. |
| Token/session leaks through logs or events | Observability never records bodies/headers/cookies; generic errors; tests use canaries. |
| Session database compromise reveals usable cookies | Store SHA-256 digest only; raw id exists only in the HttpOnly cookie. |
| Session fixation | Ignore supplied cookie on login and always generate a new cryptographic id. |
| Session replay after logout/expiry/restart | Digest removed on logout, checked against expiry, in-memory store disappears on restart. |
| Unbounded session growth | Global cap 128; expired entries pruned before admission; oldest expiry evicted if needed. |
| `GET /console` with existing bearer/OAuth auth creates session state | Accepted bootstrap behavior: the request already carries valid existing authorization, always mints a fresh opaque session, redirects to the clean `/console` URL, and grants no authority beyond the existing credential. |
| CSRF logout/login abuse | SameSite=Strict cookie; login creates a new session only after secret validation; logout only clears/revokes current session. |
| Brute-force login | Constant-time compare, bounded body, generic response; deployment should retain reverse-proxy rate limiting. No username oracle exists. |
| XSS through runtime values | Runtime fields are encoded into JSON and text nodes; no `innerHTML`; HTML is static embedded content. |
| Supply-chain/front-end compromise | No npm, CDN, remote font, analytics, service worker, or third-party script. |
| Clickjacking or data exfiltration | `frame-ancestors 'none'`, `X-Frame-Options: DENY`, restrictive CSP/connect-src, no referrer. |
| Console becomes a shadow control plane | Fixed status schema; no generic proxy, JSON-RPC, repository, audit, plan, deployment, or configuration endpoint. |
| Legacy query token reaches asset URLs | Console form/session flow does not propagate `?key=`; direct existing auth may render the page but assets/status require session or bearer/OAuth per request. |
| Sensitive status expansion later | Tests enumerate exact JSON keys and reject maps/free-form fields. |

## Accepted residual risk

- A stolen valid console cookie grants read-only access to public runtime identity until
  logout, expiry, or restart.
- A stolen MCP bearer token already grants MCP authentication and can also create a
  console session; P8 does not increase that credential's existing authority.
- `GET /console` is intentionally state-creating only when the request already passes
  existing bearer/OAuth authorization. This exception is accepted to remove tokens
  from subsequent browser requests and redirects immediately to a clean URL.
- Global login rate limiting remains a reverse-proxy/operator responsibility; the
  application adds no IP/identity tracking because those values are private and would
  create a new data store.

## Stop conditions

P8 must not ship if any test shows token/cookie exposure, unauthenticated status/assets,
missing browser security headers, arbitrary status fields, external resources, or any
route capable of invoking MCP tools or private integrations.
