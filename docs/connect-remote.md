# Connect MCP Devbox to ChatGPT and local MCP clients

MCP Devbox supports two transports:

- **stdio:** a local MCP client starts the server on the same machine;
- **HTTP:** authenticated MCP over `GET/POST /mcp` behind a stable HTTPS origin.

OAuth is the preferred ChatGPT path. A static bearer remains a **header-only recovery**
mechanism for clients that can protect an `Authorization` header. Query-string
credentials are rejected.

The complete configuration inventory is canonical in
[`configuration.md`](configuration.md). This guide lists only the settings needed for
connection.

## Local stdio

Start with `read-only`:

```bash
mcp-devbox serve --root /absolute/path/to/repository --mode read-only
```

Use `ask` only when the client must patch, run tests, or commit:

```bash
mcp-devbox serve \
  --root /absolute/path/to/repository \
  --mode ask \
  --test-cmd "go test ./... -count=1" \
  --allow-cmd git,go
```

A local client configuration uses the normal `mcpServers` shape. Example:

```json
{
  "mcpServers": {
    "mcp-devbox": {
      "command": "/absolute/path/to/mcp-devbox",
      "args": [
        "serve",
        "--root",
        "/absolute/path/to/repository",
        "--mode",
        "read-only"
      ]
    }
  }
}
```

Use the platform-appropriate binary path. The direct binary requires at least one
absolute `--root`.

## Local HTTP

HTTP requires either OAuth or a recovery bearer. For a disposable loopback-only
instance, a bearer can be supplied through the environment rather than process argv:

```bash
MCP_DEVBOX_TOKEN=REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE \
  mcp-devbox serve \
    --root /absolute/path/to/repository \
    --mode read-only \
    --http :8765
```

A hostless listener binds to loopback. Do not expose it directly to a network.

Basic smoke:

```bash
curl -i http://127.0.0.1:8765/healthz
curl -i http://127.0.0.1:8765/version
curl -i http://127.0.0.1:8765/mcp
curl -i -H "Authorization: Bearer ${MCP_DEVBOX_TOKEN}" \
  http://127.0.0.1:8765/mcp
curl -i "http://127.0.0.1:8765/mcp?key=${MCP_DEVBOX_TOKEN}"
```

Expected:

- `/healthz` returns `200`;
- `/version` returns bounded live build/catalog identity;
- unauthenticated `/mcp` returns `401`;
- the correct bearer in `Authorization` authorizes the recovery path;
- query-string credentials return `401`, even when correct.

## Public HTTPS and OAuth

A public deployment must place the internal listener behind TLS. Configure the public
issuer and owner passphrase in the platform secret/environment manager:

```text
MCP_DEVBOX_PUBLIC_URL=https://mcp.example.com
MCP_DEVBOX_OAUTH_PASSPHRASE=REPLACE_WITH_LONG_OWNER_PASSPHRASE
MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json
MCP_DEVBOX_OAUTH_REFRESH_STORE=/state/oauth-refresh.json
```

The public URL and passphrase must be configured together. Persist the client and
refresh stores on `/state` so a rolling replacement does not require connector
recreation or owner login. Access tokens and authorization codes are not copied into
repository storage.

See [`oauth.md`](oauth.md) for the protocol contract and
[`deploy-coolify.md`](deploy-coolify.md) for production deployment.

## ChatGPT connector

Configure the clean endpoint:

```text
https://mcp.example.com/mcp
```

1. Open ChatGPT connector/app settings and create an MCP connection.
2. Enter the clean `/mcp` URL without credentials in the query string.
3. Select OAuth.
4. Complete the owner authorization step.
5. After connection, call a read-only tool and verify the server identity with
   `system_runtime_info`.

A ChatGPT connector generally cannot inject an arbitrary recovery bearer. That is
expected; use OAuth.

## Other remote MCP clients

A client that supports protected custom headers may use:

```text
Authorization: Bearer REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE
```

Do not store that value in a repository, client-export file, URL, prompt, screenshot,
or shell history. Prefer OAuth for persistent public use.

## Tool discovery and operating flow

Do not copy a tool table into client setup. The canonical public catalog is
[`tools.md`](tools.md), while the live server publishes its schemas through MCP
discovery. `/version` and `system_runtime_info` identify the live build and catalog.

A reliable repository workflow is:

```text
workspace_checkpoint
→ build_context_pack when file context is needed
→ read/search
→ apply_patch/create_file
→ focused validation
→ complete validation
→ commit
→ planned publication/PR/merge only when requested
```

Direct reads and bounded status operations execute under policy and audit. Publication,
deployment, creation, merge, and similar consequential actions use their documented
preview, single-use plan, approval, revalidation, execution, and audit flow.

## Reverse proxy or tunnel

A local loopback server can be exposed through a reviewed TLS reverse proxy or outbound
tunnel. The proxy must preserve:

- OAuth discovery and `/oauth/*` routes;
- authenticated `GET/POST /mcp`;
- `/healthz` and `/version`;
- streaming responses and normal timeout behavior;
- rejection of query-string credentials.

Do not rely on obscurity or a random URL as authentication. An additional identity gate
may be used, but it does not replace MCP Devbox authentication and policy.

## MCP Inspector

For diagnostics:

```bash
npx @modelcontextprotocol/inspector
```

Point it at the clean `/mcp` URL, use Streamable HTTP, complete OAuth or add a protected
recovery header, then call a normal read-only tool and confirm redaction.

## Troubleshooting

| Symptom | Cause / action |
|---|---|
| `401` from `/mcp` | Complete OAuth or send the configured recovery bearer in `Authorization`. Query credentials never authorize. |
| Connector cannot send a bearer header | Configure OAuth on the clean URL. |
| `/healthz` works but connector is stale | Compare `/version` or `system_runtime_info` with the expected exact commit, then refresh the client catalog. |
| No tools appear | Verify the URL, OAuth completion, reverse-proxy routing to internal port `8765`, and MCP initialization. |
| Tool reports outside the jail | The selected path is not under a configured root. |
| Secret read returns `access-required` | Expected. Only the local human grant flow can approve it. |
| Secret appears unredacted | Treat it as a security vulnerability and follow `../SECURITY.md`. |

Connection authentication is only the entrance gate. Repository jail, secret handling,
mode, allowlists, plans, redaction, and audit still govern every tool call.
