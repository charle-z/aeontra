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

A later probe failure closes admission to the backend without immediately failing the
client request. New requests wait at the front door for at most the configured bounded
admission timeout. Their bodies are not sent upstream while the backend is unavailable;
compatible readiness wakes them immediately. Once a `POST /mcp` has been dispatched,
the front door never retries it because the backend may have executed the JSON-RPC
operation even when its response was lost.

Requests already accepted continue on the backend connection that accepted them. For
an authenticated `GET /mcp` SSE stream, the front door owns the downstream connection.
If the backend process retires, it keeps the client stream open, waits for the next
compatible backend and reconnects upstream with the same authorization and durable
session headers. This reconnect contract is deliberately limited to the current MCP
backend stream, which emits comments and keepalives only. A non-comment SSE event fails
closed until an explicit replay or resume contract exists.

A proxied `/mcp` response is also rejected if its `X-MCP-Catalog-Hash` differs from the
pinned contract.

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
MCP_FRONT_DOOR_ADMISSION_TIMEOUT=45s
```

Only an HTTPS backend origin is accepted, except loopback HTTP for local validation.
User information, query strings, fragments and backend path prefixes are rejected.

## Routes

- `/front-door/healthz`: front-door process liveness; independent from backend health.
- `/front-door/readyz`: front door plus compatible backend readiness.
- `/front-door/version`: bounded front-door identity, last compatible backend state and
  aggregate admission/recovery counters. It contains no request body, credential,
  session identifier, hostname, IP address or raw transport error.
- every other route, including `/mcp`, OAuth discovery, authorization, token exchange,
  `/console`, `/healthz` and `/version`, is proxied to the compatible backend.

Use `/front-door/healthz` for the front-door container healthcheck. Do not use backend
`/readyz` as the platform liveness check for the front door, because a backend rollout
must not cause the platform to restart the stable facade.

## Managed Coolify workflow

MCP Devbox exposes three narrow operations for the first independent deployment:

1. `platform_front_door_create_preview` validates the temporary public origin, fixed
   backend origin, exact protocol and catalog hash, then binds the current commit of
   `front-door-stable` into an expiring single-use plan.
2. `platform_front_door_create` creates or reconciles exactly one application named
   `mcp-devbox-front-door-managed`, upserts the three non-secret compatibility
   variables, and deploys only when the pinned commit is not already healthy.
3. `platform_front_door_status` resolves that fixed application by server-owned name
   and returns bounded deployment-contract metadata without exposing environment
   values or requiring its UUID in the general application allowlist.

The caller cannot select a repository, branch, Dockerfile, port, mounts, destination,
healthcheck, Docker flags, auto-deploy posture or application name. Those values are
compiled into the managed contract. Duplicate names, a changed stable-branch SHA, an
existing application with different topology, or a domain outside
`COOLIFY_ALLOWED_DOMAINS` fail closed.

## Managed DuckDNS topology and reversible cutover

The production topology reuses the existing DuckDNS name and its subdomains:

```text
public connector: https://mcp-devbox-charlez.duckdns.org
front-door staging: https://front.mcp-devbox-charlez.duckdns.org
backend origin: https://backend.mcp-devbox-charlez.duckdns.org
```

All three names resolve to the same VPS. Traefik routes by hostname, so the front door
and backend remain separate applications even though they share one IP address. The
front door must never use its own public hostname as its backend origin.

The existing `platform_front_door_create_preview` and
`platform_front_door_create` operations recognize three fixed transitions without
adding a generic domain editor or changing the MCP catalog:

1. `rename-temporary`: replace the legacy `sslip.io` front-door hostname with the
   fixed `front.` DuckDNS hostname. If readiness fails, restore the legacy hostname.
2. `cutover`: start the normal backend routing deployment that adds the `backend.`
   hostname while the original public hostname remains active, then return its
   deployment ID before that backend replaces the process executing the tool.
3. `resume-cutover-backend`: after the caller verifies that deployment reached a
   successful terminal state, verify the alternate backend, redeploy the front door
   against it, and start the normal backend deployment that releases the public
   hostname.
4. `resume-cutover-public`: after the caller verifies the backend-release deployment
   reached a successful terminal state, verify the temporary front door, assign the
   public hostname to it, and verify the final topology. Each resume action is derived
   from the exact external topology and does not depend on the prior in-memory plan.
5. `rollback`: move the front door back to `front.`, restore and verify the original
   hostname on the backend, redeploy the front door against it, and remove the
   alternate backend hostname.

The backend application UUID, all three origins, application identities, branch,
repository, protocol, catalog hash, deployment mode and operation order are compiled
into the managed contract. The caller cannot supply another backend application or an
arbitrary migration topology.

The production public-domain cutover and rollback are executed by a private coordinator
worker, not by the backend request whose hostname may disappear during the transition.

The coordinator contract is exposed through five closed operations:

1. platform_front_door_coordinator_preview binds the current main commit, frozen
   facade commit, exact backend commit, fixed application UUIDs, protocol and catalog
   into a single-use plan.
2. platform_front_door_coordinator_create creates or reconciles one private worker
   with no public domain, one dedicated persistent journal and the exact private
   host-gateway mapping required to reach the operator-owned Coolify API without
   sending its token to a public HTTP destination.
3. platform_front_door_transition_preview reconstructs the real topology, verifies
   healthy finished deployments, exact branch commits and the complete managed worker
   environment, then returns a dispatch, observe or noop disposition for cutover or
   rollback. The same identity is fixed in the single-use plan and revalidated at execute.

Application health comes from each Coolify application record, but deployment state and
commit identity do not. Current Coolify versions may expose `deployment_status=null` and
`git_commit_sha=HEAD` on an otherwise healthy application. The gate therefore queries the
application deployment history, selects one unambiguous latest deployment by timestamp,
requires terminal `finished`, and seals its exact commit into the preview. Backend and
front-door deployments must equal the current approved branch commits. The coordinator
may run an earlier reviewed `main` commit, but that commit must remain an ancestor of the
current `main`; a divergent, active, failed, ambiguous or malformed deployment fails closed.
The coordinator ancestry check uses GitHub's compare endpoint with one commit per page and a
dedicated 8 MiB response cap because compare responses may include file metadata far larger
than a branch-ref response. Other GitHub ref and merge operations retain their 64 KiB cap,
and an oversized or malformed compare response still fails closed.
If the optional paginated deployment request returns a successful but empty body, the gate
performs exactly one compatibility read of the same official endpoint without pagination.
A second empty response, non-success status, malformed non-empty JSON or an oversized record
set still fails closed; no application or transition is modified by either read.
The primary request asks Coolify for only the two newest deployments because the upstream
model orders them by `created_at` descending; two records are sufficient to identify the
latest entry and detect a timestamp tie. Deployment history uses a dedicated 32 MiB response
cap with explicit overflow detection rather than the generic 1 MiB response reader. Large
upstream `logs` fields are ignored by the bounded decoder and are never returned or logged.
4. platform_front_door_transition may only set that closed target, bind the consumed
   single-use plan ID as the durable request ID and trigger one normal deployment of
   the coordinator. It does not patch facade or backend domains.
5. platform_front_door_transition_status reads the bounded published journal and the
   current fixed topology without exposing environment values or credentials.

Startup readiness exposes only closed diagnostic codes. Private host-gateway transport
failures distinguish an invalid fixed target, gateway resolution, address-policy
rejection, connection refusal, timeout, unavailable route and an otherwise unclassified
connection failure. The generic transport code remains the fallback for non-gateway
transports. None of these codes includes the Coolify origin, host, IP, port, token,
response body or raw network error.

After Coolify removes an unhealthy coordinator container, `platform_deployment_status`
may recover only one unambiguous allowlisted coordinator `safe_code` from the retained
deployment record. It never returns the underlying deployment log or arbitrary matches.

The worker has no connector hostname and is not a second facade. It stores an atomic,
worker-private monotonic journal under /coordinator-state, accepts only the two fixed
targets and can mutate only the two compiled application UUIDs and three compiled
DuckDNS origins.

Each phase performs one normal non-force deployment, waits for its terminal state and
verifies the expected origin before advancing. A restart resumes from the journal and
the externally visible topology. Unknown topology, conflicting active targets, missing
storage, duplicate application identity or an exhausted finite phase budget fail closed.

An active `queued`, `running` or `compensating` request in the persistent journal is
authoritative across container replacement, even when the managed environment has already
been reconciled back to `idle`. A replacement worker restores that exact request ID and target
from the journal; it never substitutes a new request. The worker reports ready first and waits
75 seconds before resuming, so Coolify can complete the coordinator rollout before the worker
publishes status or mutates the managed topology. Cancellation during that gate performs no
transition work.

A non-interruption failure changes the durable state to `compensating` and drives the
opposite fixed target: failed cutover restores the direct-backend topology; failed
rollback restores the stable-front-door topology. Compensation derives every next phase
from the current external domains and facade backend, retries transient failures inside
a finite budget and resumes after process replacement. Once the safe topology is
restored, the original request remains terminal `failed` with a `_compensated` reason;
it is never reported as a successful cutover or rollback.

Published status is retried with a finite budget. If publication or the local journal
remains unavailable, the worker exits non-zero instead of remaining healthy with a
stalled transition, so the platform can surface or restart it from the durable journal.

The persistent volume remains the complete authoritative journal. Coolify's application
description carries only a versioned compact observation envelope capped at 255 ASCII bytes:
revision, request ID, target, recovery target, state, phase, deployment ID, one reason from the
fixed transition dictionary and update time. Live domains and facade upstream are read directly
from the managed applications instead of being duplicated in that envelope. New workers publish
`mcp-fdc:v2`; readers retain strict compatibility with the previous `v1` JSON description.
Unknown fields, reason codes, identifiers, trailing data or oversized envelopes fail closed.
Publication failures preserve only an enumerated safe cause such as request build, private
gateway transport, response read, HTTP or decode failure; no response body, token or URL is
reported.

The backend-facing dispatch remains safe if GPT Web temporarily loses the MCP namespace:
the independent worker continues from its durable journal. The same request ID resumes
or republishes its existing state; it never restarts a failed transition. Only a new
reviewed preview and dispatch may retry a failed target, and a different request cannot
replace one already queued, running or compensating. The request ID is not returned by
status tools.
After reconnecting, callers must read transition status before considering another
dispatch; they must never repeat a write merely because the client reported a network
error.

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
- The private coordinator receives the Coolify token only through its managed environment;
  its client can mutate only the two compiled application UUIDs and fixed topology fields.
- The backend URL is startup configuration, never caller input.
- The front door has no repository volumes, state volume, Docker socket or Edge access.
- It is not a generic outbound proxy and does not accept arbitrary target URLs.
- Admission waiting is finite. If no compatible backend returns within the configured
  budget, the request fails closed with `503` and `Retry-After`; it is not forwarded.
- There is no generic write retry. A `POST /mcp` transport error after dispatch is
  returned as unavailable rather than risking a duplicated tool execution.
- SSE reconnection is safe only while the backend emits comments and keepalives. The
  proxy rejects a data-bearing event instead of silently losing or duplicating it.
- It cannot prevent a ChatGPT client-side catalog/cache or namespace presentation issue.
- A healthy front door plus healthy backend during a missing namespace is evidence of a
  client presentation problem, not proof of an internal OpenAI root cause.

See [`configuration.md`](configuration.md) for the canonical variable inventory and
[`runbooks/client-connector-reliability.md`](runbooks/client-connector-reliability.md)
for incident classification.

When the managed Docker alias is unavailable, the private client derives the single
usable default gateway from a bounded container route table and accepts it only when
it is a private IPv4 address. Missing, malformed, public, ambiguous or non-gateway
routes retain the closed resolution failure.
