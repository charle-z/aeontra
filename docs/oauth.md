# OAuth for the ChatGPT connector

mcp-devbox can authenticate the HTTP transport with **OAuth 2.1**, so ChatGPT (and other
MCP clients that speak the [MCP Authorization spec](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization))
connect via their standard **OAuth** connector option — no secret in the URL.

The daemon is its **own** authorization server (single-owner, in-process, zero external
dependencies). It is also the resource server that validates access tokens on `/mcp`.

## Enabling it

Set both env vars (OAuth stays **off** unless both are present):

| Env | Meaning | Example |
|-----|---------|---------|
| `MCP_DEVBOX_PUBLIC_URL` | Public HTTPS base URL (the OAuth **issuer**). Must be `https://` (only `localhost` may use `http`). | `https://mcp-devbox-charlez.duckdns.org` |
| `MCP_DEVBOX_OAUTH_PASSPHRASE` | The owner login secret entered on the authorize page. | *(a strong passphrase)* |
| `MCP_DEVBOX_OAUTH_CLIENT_STORE` | Optional JSON file for persistent Dynamic Client Registration clients. Store it on a persistent volume outside the MCP repo jail when possible. | `/state/oauth-clients.json` |
| `MCP_DEVBOX_OAUTH_REFRESH_STORE` | Optional JSON file (0600) that persists **only refresh tokens**, so a redeploy/restart does **not** force the passphrase login again — ChatGPT's stored refresh token still works. Access tokens and authorization codes are never persisted. Put it on the same persistent volume. | `/state/oauth-refresh.json` |

The token **audience** (canonical resource) is derived as `<PUBLIC_URL>/mcp`.

### Avoiding re-login on every redeploy

Refresh tokens live in memory unless `MCP_DEVBOX_OAUTH_REFRESH_STORE` points at a file on
a **persistent volume**. Without it, every redeploy drops all tokens and ChatGPT must
re-enter the passphrase. With both `..._CLIENT_STORE` and `..._REFRESH_STORE` on a volume,
a redeploy is seamless: the connector silently refreshes and keeps working.

### Confirming which commit is live

`GET /healthz` returns `ok mcp-devbox <version> <commit>` and the MCP `initialize`
response includes `serverInfo.commit`. The commit is baked at build time
(`--build-arg GIT_SHA=$(git rev-parse HEAD)`) or read at startup from `SOURCE_COMMIT`
(injected by Coolify) / `MCP_DEVBOX_COMMIT`. Compare it against `git rev-parse HEAD` on
`main` to verify a redeploy actually shipped the latest code.

`MCP_DEVBOX_TOKEN` (the legacy static bearer / `?key=`) is now **optional**:

- **OAuth on** → you may drop the static token entirely (OAuth-only). If you keep it, it
  still works as a fallback during the transition.
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
- **Narrow persistence only**: if `MCP_DEVBOX_OAUTH_CLIENT_STORE` is set, DCR public
  client registrations persist so ChatGPT can reauthenticate after redeploy without
  deleting the connector. Authorization codes, access tokens, and refresh tokens stay
  in process memory only; a restart drops sessions and forces a fresh owner login.
- Keep the client store file on a small persistent volume such as `/state`, not inside
  the writable repo workspace, so MCP tools cannot edit OAuth server state as data.

## Connecting ChatGPT

1. Deploy with the two required env vars set (e.g. in Coolify). For stable reconnects
   after redeploy, also set `MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json`
   and mount `/state` as a persistent volume.
2. In ChatGPT, add the connector with the MCP URL `https://<host>/mcp` and choose the
   **OAuth** option (no manual client id needed — DCR handles it).
3. ChatGPT discovers the AS, registers, and opens the authorize page; enter the
   passphrase; it exchanges the code for a token and connects.

## After OAuth is verified

Rotate `MCP_DEVBOX_TOKEN` (it was shared during setup) and, once OAuth works end-to-end,
you may remove the static token to make the server **OAuth-only**.
