# Catalog-aware backend rollout

The public MCP backend and the stable Front Door share a strict protocol and catalog
contract. A backend catalog change must never reach the official domain before the Front
Door admits it.

## Authority and entry point

The existing `platform_deploy_preview` / `platform_deploy` pair remains the only public
entry point. When the selected application is the fixed managed MCP backend, the service
uses the catalog-aware path instead of calling Coolify deploy directly. No new MCP tool,
public endpoint, arbitrary hash input, or caller-provided deployment target is added.

`platform_deploy_without_cache` and the legacy force-deploy alias reject the managed
backend. Other applications retain the generic deployment behavior.

## Candidate identity

The candidate is always the exact owner-bound `main` SHA. Its catalog identity comes
from `deploy/catalog-identity.json` at that exact SHA. The manifest is not trusted by
itself: the `Catalog Identity / Verify catalog identity` workflow recalculates the
protocol, tool count, and hash from source with `mcp-catalog-id --verify`.

A rollout requires complete exact-head GitHub evidence with no pending or failed checks.
Mutable refs, malformed hashes, wildcard values, protocol changes, incomplete evidence,
and more than two admitted catalogs fail closed.

## Managed sequence

The private Front Door coordinator owns the durable state machine:

1. Observe the current backend, Front Door, Coolify application identity, commit pin,
   auto-deploy state, protocol, tool count, and catalog.
2. Pin the backend to the currently compatible commit and set both
   `is_auto_deploy_enabled=false` and `instant_deploy=false`.
3. If the catalog is unchanged, deploy the exact candidate once and verify it.
4. If the catalog changed, deploy the Front Door with exactly
   `primary=candidate` and `transition=previous`, then verify the old backend remains
   reachable through the official domain.
5. Deploy the exact backend candidate once.
6. Verify the direct backend and official Front Door identities, OAuth discovery, one
   authenticated MCP initialize, the same MCP session, and a real
   `system_runtime_info` tool call.
7. Reconcile the Front Door to candidate-only and verify the old catalog is retired.

The coordinator writes an atomic journal under its existing private persistent volume
and publishes only a compact redacted status in the coordinator application description.
It resumes from observation and journal state rather than repeating actions blindly.

## Failure behavior

- Failure before the backend switch restores the previous Front Door contract.
- A failed backend deployment preserves its opaque deployment ID, explicitly repins and
  redeploys the previous compatible commit before observation, and verifies the restored
  backend and Front Door identities. A failed recovery preserves the recovery deployment
  ID instead.
- Failure after the backend switch deploys the previous compatible commit and restores
  the previous Front Door contract.
- A different active request is rejected.
- A topology transition and a catalog rollout cannot run at the same time.
- The official domain is never pointed at a backend that is outside the admitted pair.
- OAuth discovery remains independent from the MCP catalog gate; `/mcp` remains
  fail-closed when the backend catalog is not admitted.

## Operator runbook

Before a rollout:

```text
system_runtime_info
platform_front_door_status
platform_front_door_transition_status
platform_deploy_preview(app=<managed backend UUID>)
```

Review the previous/candidate commits, protocol, tool counts, hashes, exact-head check
evidence, and coordinator identity. Then execute the single-use plan with approval.

Observe the coordinator application and deployment status until the compact catalog
rollout state is terminal. A successful rollout ends with:

- backend commit equal to the candidate;
- protocol/tool count/catalog equal to the candidate manifest;
- Front Door healthy and candidate-only;
- backend and Front Door auto-deploy bypasses disabled;
- OAuth discovery valid;
- real MCP smoke valid.

Do not edit the Front Door allowlist manually, add a third catalog, enable wildcard
matching, re-enable backend auto-deploy, or trigger a direct Coolify deployment while a
rollout is active.

## Authoritative simulation

`internal/catalogrollout` tests the durable state machine and compensation. The
`internal/frontdoorcoordinator` fixture proves the unchanged and changed paths, exact
one-time backend deployment, two-phase Front Door reconciliation, OAuth/MCP verification,
and rollback after a post-switch MCP failure. The real Front Door handler tests retain
the OAuth-during-mismatch, `/mcp` fail-closed, two-catalog maximum, and SSE/session
continuity contracts.
