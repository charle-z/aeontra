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
2. Create or non-force fast-forward the fixed, server-owned
   `backend-rollback-stable` branch to the currently running backend commit. The caller
   cannot select or move this branch.
3. Pin the backend to the candidate on `main` and keep both
   `is_auto_deploy_enabled=false` and `instant_deploy=false`.
4. If the catalog changed, deploy the Front Door with exactly
   `primary=candidate` and `transition=previous`, then verify the old backend remains
   reachable through the official domain.
5. Stop the singleton backend through the Coolify application API, wait for its
   container to exit, deploy the candidate exactly once, and verify it. This bounded
   stop-first replacement is required because the durable SQLite stores permit one
   writer and cannot participate in Coolify's overlapping rolling replacement.
6. Verify the direct backend and official Front Door identities, OAuth discovery, one
   authenticated MCP initialize, the same MCP session, and a real
   `system_runtime_info` tool call.
7. Reconcile the Front Door to candidate-only and verify the old catalog is retired.

The coordinator writes an atomic journal under its existing private persistent volume
and publishes only a compact redacted status in the coordinator application description.
It resumes from observation and journal state rather than repeating actions blindly.

## Failure behavior

- Failure before the backend switch restores the previous Front Door contract.
- A failed backend deployment preserves its opaque deployment ID, switches temporarily
  to the fixed rollback branch, stops any active backend, deploys the previous compatible
  commit, restores the application metadata branch to `main`, and verifies the recovered
  backend and Front Door identities. A failed recovery preserves the recovery deployment
  ID instead.
- Failure after the backend switch uses the same fixed-branch stop-first recovery and
  restores the previous Front Door contract.
- A different active request is rejected.
- A topology transition and a catalog rollout cannot run at the same time.
- The official domain is never pointed at a backend that is outside the admitted pair.
- OAuth discovery remains independent from the MCP catalog gate; `/mcp` remains
  fail-closed when the backend catalog is not admitted.
- The stable Front Door and OAuth routes remain live during the stop-first interval, but
  authenticated MCP calls can be temporarily unavailable. This workflow does not claim
  zero-downtime backend replacement.

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
