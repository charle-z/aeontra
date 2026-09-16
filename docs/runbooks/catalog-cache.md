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

That check does not authenticate or execute MCP discovery. After deployment, run the
routing smoke with the recovery credential supplied only through the environment:

```bash
MCP_DEVBOX_TOKEN="..." go run ./cmd/mcp-routing-smoke \
  --url https://mcp.example.com \
  --expected-commit "$(git rev-parse HEAD)"
```

The routing smoke creates a session, materializes the complete `tools/list`, calls
`system_runtime_info`, calls `sandbox_status`, and verifies that all transport identities
agree. It renews a session at most once and only after HTTP `404`. It never retries MCP
application errors. `--sandbox-cwd` additionally performs read-only `pwd`, Git identity,
and before/after worktree checks in one authorized repository.

## Tool-list change behavior

The production catalog is immutable for the lifetime of one server process. The server
therefore advertises `capabilities.tools.listChanged=false` and does not fabricate a
`notifications/tools/list_changed` event merely because a container restarted.

A real contractual change is deployed as a replacement instance with a different
catalog hash. Configured durable session storage can preserve the logical session across
that replacement; the current catalog hash is recorded when the session is used again.
This does not make a client refresh a previously materialized tool snapshot. A client
must still run `initialize` and request `tools/list` to observe a new contract. The
server cannot force a connected client to remount or refresh an app inside an existing
conversation.

## Diagnosis

### Smoke check fails on commit

The new source was not deployed. Check the webhook, branch, build cache, deployment
status, and live container commit. Do not reconnect clients yet.

### Commit matches but catalog hash/count differ

The running binary and the local source do not represent the same catalog. Check
build context, generated sources, branch state, or an incomplete image replacement.
Do not classify this as a client cache problem.

### Commit, hash, and count all match but the client lacks tools

The server is current. Run the authenticated routing smoke. If it lists and invokes the
full catalog, reselect or reconnect the app once so the client initializes and requests
`tools/list` again. Record the client/version behavior as a compatibility limitation
rather than redeploying repeatedly.

### OAuth asks for login after every deployment

That is separate from catalog caching. Verify the persistent OAuth client and refresh
stores are mounted and configured. Do not delete the connector as a cache-clearing
strategy unless its registration is actually invalid.

## Rollback

Roll back to the prior commit if `/version`, HTTP headers, initialization, session
replacement, or SSE keep-alive behavior causes a client regression. Existing auth,
authority, jail, grant, and durable-state mechanisms remain unchanged.
