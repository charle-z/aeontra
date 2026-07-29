# Runbook: deployment identity and stale MCP catalogs

Use this runbook after changing tool names, descriptions, schemas, annotations, aliases,
or contract versions. It distinguishes a stale deployment from a client that retained
an older `tools/list` result.

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

## Tool-list change behavior

The production catalog is immutable for the lifetime of one server process. The server
therefore advertises `capabilities.tools.listChanged=false` and does not fabricate a
`notifications/tools/list_changed` event merely because a container restarted.

A real contractual change is deployed as a replacement instance with a different
catalog hash. Sessions are instance-bound, so the previous session fails and a client
must run `initialize` again on the same URL and OAuth configuration, then request
`tools/list`. The server cannot force ChatGPT or another client to reload a connector
inside an existing conversation.

## Diagnosis

### Smoke check fails on commit

The new source was not deployed. Check the webhook, branch, build cache, deployment
status, and live container commit. Do not reconnect clients yet.

### Commit matches but catalog hash/count differ

The running binary and the local source do not represent the same catalog. Check
build context, generated sources, branch state, or an incomplete image replacement.
Do not classify this as a client cache problem.

### Commit, hash, and count all match but the client lacks tools

The server is current. Confirm the old session was rejected, the client received a new
`initialize` response, and the new session requested `tools/list`. Reconnect the
connector once if the client does not start a new session. Record the client/version
behavior as a compatibility limitation rather than redeploying repeatedly.

### OAuth asks for login after every deployment

That is separate from catalog caching. Verify the persistent OAuth client and refresh
stores are mounted and configured. Do not delete the connector as a cache-clearing
strategy unless its registration is actually invalid.

## Rollback

Roll back to the prior commit if `/version`, HTTP headers, initialization, session
replacement, or SSE keep-alive behavior causes a client regression. Existing auth,
authority, jail, grant, and durable-state mechanisms remain unchanged.
