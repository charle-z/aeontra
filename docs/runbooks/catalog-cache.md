# Runbook: deployment identity and stale MCP catalogs

Use this runbook after changing tool names, schemas, annotations, aliases, or contract
versions. It distinguishes a stale deployment from a client that retained an older
`tools/list` result.

## Runtime signals

The live server exposes three matching forms of non-sensitive identity:

1. `GET /version` JSON.
2. HTTP response headers:
   - `X-MCP-Server-Commit`
   - `X-MCP-Catalog-Hash`
   - `X-MCP-Tool-Count`
3. MCP `initialize.serverInfo`:
   - `commit`
   - `builtAt`
   - `toolCount`
   - `catalogHash`

Dynamic HTTP responses use `Cache-Control: no-store` and `Pragma: no-cache`.

## Automated smoke check

From the exact source commit expected in production:

```bash
go run ./cmd/mcp-catalog-smoke \
  --url https://mcp.example.com \
  --expected-commit "$(git rev-parse HEAD)"
```

The command:

- requires HTTPS except for loopback tests;
- follows no redirects;
- sends no token;
- reads at most 64 KiB;
- uses a bounded timeout;
- compares semantic version and protocol version;
- compares exact commit;
- compares deterministic tool count and catalog hash;
- verifies body/header consistency and no-cache headers;
- prints no server configuration or secrets.

Success means the deployed process is running the expected source and catalog.

## Tool-list change notification

The server advertises `capabilities.tools.listChanged=true`. On the first authorized
SSE stream opened after a process starts, it emits one JSON-RPC notification:

```json
{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}
```

The notification is one-shot per process so the same event is not duplicated across
multiple concurrent SSE streams. A compatible client should request `tools/list`
again. This is best effort: the server cannot force a client to discard an internal
catalog cache.

## Diagnosis

### Smoke check fails on commit

The new source was not deployed. Check the webhook, branch, build cache, deployment
status, and live container commit. Do not reconnect clients yet.

### Commit matches but catalog hash/count differ

The running binary and the local source do not represent the same catalog. Check
build context, generated sources, branch state, or an incomplete image replacement.
Do not classify this as a client cache problem.

### Commit, hash, and count all match but the client lacks tools

The server is current. Confirm the client received a new `initialize` response and
an SSE tool-list change notification. Reconnect the connector once if it does not
request `tools/list` again. Record the client/version behavior as a compatibility
limitation rather than redeploying repeatedly.

### OAuth asks for login after every deployment

That is separate from catalog caching. Verify the persistent OAuth client and refresh
stores are mounted and configured. Do not delete the connector as a cache-clearing
strategy unless its registration is actually invalid.

## Rollback

The catalog features are additive. Roll back to the prior commit if `/version`, HTTP
headers, initialization, or SSE behavior causes a client regression. Existing tool
names, schemas, handlers, environment variables, and auth mechanisms are unchanged.
