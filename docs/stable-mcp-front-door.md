# Stable MCP Front Door

The Stable MCP Front Door is a stateless MCP-aware reverse proxy deployed independently
from the full MCP Devbox control plane. It reduces connector interruptions caused by
backend container replacement without pretending to control the ChatGPT client.

## Boundary

```text
MCP client
   │  stable public URL, TLS, OAuth and MCP headers
   ▼
Stable MCP Front Door
   │  fixed operator-owned backend origin
   ▼
MCP Devbox control plane
   ├─ OAuth and durable MCP sessions
   ├─ tools and policy
   └─ durable repositories, Brain, results and Edge coordination
```

The front door does not terminate OAuth, create MCP sessions, inspect JSON-RPC bodies,
store credentials, or own repository/Edge authority. It forwards the original request
to one fixed backend origin and preserves `Authorization`, `Mcp-Session-Id`, response
headers, streaming bodies and status codes. The backend remains authoritative for every
security and tool decision.

## Compatibility gate

The process refuses new proxied requests until both backend probes pass:

1. `GET /readyz` returns `200`;
2. `GET /version` reports `status=ok`, the configured MCP protocol version, the exact
   configured catalog hash, a valid commit and a non-empty catalog.

A later probe failure stops only new requests. Requests already accepted continue on
the backend connection that accepted them. A proxied `/mcp` response is also rejected
if its `X-MCP-Catalog-Hash` differs from the pinned contract.

This behavior is intentionally fail-closed. It prevents an incompatible backend rollout
from silently changing the connector contract behind an existing front door.

## Deployment independence

Build the dedicated image with `Dockerfile.front-door`. Deploy it from a stable branch
that advances only for reviewed front-door changes. Do not configure the front-door
application to auto-deploy on every backend `main` commit.

Required variables:

```text
MCP_FRONT_DOOR_BACKEND_URL=https://backend.example.com
MCP_FRONT_DOOR_EXPECTED_PROTOCOL=2024-11-05
MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH=sha256:<64-lowercase-hex>
```

Optional variables:

```text
MCP_FRONT_DOOR_ADDR=0.0.0.0:8765
MCP_FRONT_DOOR_PROBE_INTERVAL=1s
MCP_FRONT_DOOR_PROBE_TIMEOUT=3s
```

Only an HTTPS backend origin is accepted, except loopback HTTP for local validation.
User information, query strings, fragments and backend path prefixes are rejected.

## Routes

- `/front-door/healthz`: front-door process liveness; independent from backend health.
- `/front-door/readyz`: front door plus compatible backend readiness.
- `/front-door/version`: bounded front-door identity and last compatible backend state.
- every other route, including `/mcp`, OAuth discovery, authorization, token exchange,
  `/console`, `/healthz` and `/version`, is proxied to the compatible backend.

Use `/front-door/healthz` for the front-door container healthcheck. Do not use backend
`/readyz` as the platform liveness check for the front door, because a backend rollout
must not cause the platform to restart the stable facade.

## Rollout sequence

1. Deploy the front door on a temporary reviewed hostname pointing to the current backend.
2. Verify OAuth discovery, unauthenticated `/mcp` challenge, authenticated initialize,
   session reuse, SSE, `/version`, console and catalog identity through the temporary
   hostname.
3. Perform at least two backend replacements while the front-door instance and hostname
   remain unchanged.
4. Start one durable operation before a replacement and query the same operation ID
   afterward without creating another operation.
5. Give the backend a separate stable origin.
6. Move the existing public hostname to the validated front door using a reviewed,
   reversible platform-domain operation.
7. Confirm the original connector URL still serves the same protocol and catalog.

The final hostname migration may cause one controlled client refresh. The objective is
to eliminate recurring reconnects caused by later backend deployments, not to claim
that the server can force ChatGPT to keep a connector namespace mounted.

## Security and limitations

- Credentials pass through process memory but are never logged or persisted.
- The backend URL is startup configuration, never caller input.
- The front door has no repository volumes, state volume, Docker socket or Edge access.
- It is not a generic outbound proxy and does not accept arbitrary target URLs.
- It cannot prevent a ChatGPT client-side catalog/cache or namespace presentation issue.
- A healthy front door plus healthy backend during a missing namespace is evidence of a
  client presentation problem, not proof of an internal OpenAI root cause.

See [`configuration.md`](configuration.md) for the canonical variable inventory and
[`runbooks/client-connector-reliability.md`](runbooks/client-connector-reliability.md)
for incident classification.