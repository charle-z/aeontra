# OAuth for the ChatGPT connector

mcp-devbox can authenticate the HTTP transport with **OAuth 2.1**, so ChatGPT (and other
MCP clients that speak the [MCP Authorization spec](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization))
connect via their standard **OAuth** connector option — no secret in the URL.

The daemon is its **own** authorization server (single-owner, in-process, zero external
dependencies). It is also the resource server that validates access tokens on `/mcp`.

## Enabling it

Set both required env vars (OAuth stays **off** unless both are present):

| Env | Meaning | Example |
|-----|---------|---------|
| `MCP_DEVBOX_PUBLIC_URL` | Public HTTPS base URL (the OAuth **issuer**). Must be `https://` (only `localhost` may use `http`). | `https://mcp-devbox-charlez.duckdns.org` |
| `MCP_DEVBOX_OAUTH_PASSPHRASE` | The owner login secret entered on the authorize page. | *(a strong passphrase)* |
| `MCP_DEVBOX_STATE_ROOT` | Administrator-owned durable state root. When set, missing OAuth store paths default beneath it. The Docker image sets `/state`. | `/state` |
| `MCP_DEVBOX_OAUTH_CLIENT_STORE` | Optional absolute override for persistent Dynamic Client Registration clients. Defaults to `<STATE_ROOT>/oauth-clients.json` when a state root is configured. | `/state/oauth-clients.json` |
| `MCP_DEVBOX_OAUTH_REFRESH_STORE` | Optional absolute override for the 0600 refresh-token store. Defaults to `<STATE_ROOT>/oauth-refresh.json` when a state root is configured. | `/state/oauth-refresh.json` |

The token **audience** (canonical resource) is derived as `<PUBLIC_URL>/mcp`.

### Avoiding re-login on every redeploy

The Docker deployment sets `MCP_DEVBOX_STATE_ROOT=/state`, so OAuth client registrations
and rotating refresh tokens use durable files there by default. Explicit `..._CLIENT_STORE`
and `..._REFRESH_STORE` values still take precedence when a different administrator-owned
location is required.

The `/state` path must be a persistent volume. A container-local or newly-created anonymous
state volume still disappears from the next deployment's point of view. With the same
persistent `/state` mounted across replacements, ChatGPT can silently refresh and keep the
connector authorized. Access tokens and authorization codes remain memory-only by design.

When no state root and no explicit store paths are configured, local development preserves
the previous memory-only behavior and a process restart requires authorization again.

### Confirming which commit is live

`GET /healthz` returns `ok mcp-devbox <version> <commit>` and the MCP `initialize`
response includes `serverInfo.commit`. The commit is baked at build time
(`--build-arg GIT_SHA=$(git rev-parse HEAD)`) or read at startup from `SOURCE_COMMIT`
(injected by Coolify) / `MCP_DEVBOX_COMMIT`. Compare it against `git rev-parse HEAD` on
`main` to verify a redeploy actually shipped the latest code.

`MCP_DEVBOX_TOKEN` (the static recovery bearer, header only) is **optional**:

- **OAuth on** → you may drop the static token entirely (OAuth-only). If retained, it
  works only through `Authorization: Bearer <token>` and the console recovery form.
- **OAuth off** → a static token is still **required** (the server refuses to start with
  no auth at all).

## What the server exposes (discovery)

| Path | Purpose |
|------|---------|
| `GET /.well-known/oauth-protected-resource` and `…/oauth-protected-resource/mcp` | Protected Resource Metadata (RFC 9728) → points at the authorization server |
| `GET /.well-known/oauth-authorization-server` (and `/.well-known/openid-configuration`) | Authorization Server Metadata (RFC 8414) |
| `POST /oauth/register` | Dynamic Client Registration (RFC 7591) — ChatGPT self-registers |
| `GET/POST /oauth/authorize` | Owner passphrase login → issues a single-use PKCE-bound code |
| `POST /oauth/token` | `authorization_code` (PKCE S256) and `refresh_token` (with rotation) |

On a `401` from `/mcp`, the `WWW-Authenticate` header points at the exact PRM document so
clients can bootstrap the flow.

## Security model

- **PKCE S256 required** (plain is never accepted); `resource` (RFC 8707) required on both
  authorize and token, and access tokens are **audience-bound** to `<PUBLIC_URL>/mcp`.
- Public clients only (no client secret); **refresh tokens rotate** on every use.
- Authorization codes: single-use, ~60s TTL. Access tokens: ~1h. All tokens are opaque,
  random (256-bit), and **never logged**.
- The owner passphrase is compared in **constant time** and is **rate-limited**; DCR is
  capped and rate-limited. A registered client is useless without the human passphrase.
- Redirect URIs: exact match, `https`-or-`localhost` only, no fragments/wildcards.
- **Narrow persistence only**: public client registrations and refresh grants may be
  restored from their administrator-owned 0600 stores. Authorization codes and access
  tokens stay in process memory and disappear on restart.
- Keep `/state` outside the writable repo workspace so MCP tools cannot edit OAuth server
  state as repository data.

## Connecting ChatGPT

1. Deploy with the required public URL and passphrase. Mount the configured state root as
   one persistent volume; the Docker default is `/state`.
2. In ChatGPT, add the connector with the MCP URL `https://<host>/mcp` and choose the
   **OAuth** option (no manual client id needed — DCR handles it).
3. ChatGPT discovers the AS, registers, and opens the authorize page; enter the
   passphrase; it exchanges the code for a token and connects.
4. Redeploy once and confirm the existing connector reconnects without deleting it or
   entering the passphrase again.

## After OAuth is verified

Rotate `MCP_DEVBOX_TOKEN` (it was shared during setup) and, once OAuth works end-to-end,
you may remove the static token to make the server **OAuth-only**.

## Query-string credentials

P8.1 permanently rejects `?key=<token>` with HTTP 401, even when the value matches `MCP_DEVBOX_TOKEN`. Use OAuth for ChatGPT and remote MCP clients. Use an `Authorization: Bearer` header only for explicit recovery clients that can protect headers.
