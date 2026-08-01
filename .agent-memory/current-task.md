# Current task — Stable MCP Front Door private transport diagnosis

## Gate

- Authoritative Brain gate: `stable-mcp-front-door-roadmap`.
- Gate remains open. Do not start functional Edge roadmap work.
- Preserve the existing facade `o338wpoy1254d83ud2y8p1v8` and existing private coordinator `v13i2apwnvu09ms7l77x6opk`.
- No cutover or rollback has been dispatched.

## Verified base

- PR #105 merged with exact head `714d09e350991251be338d5fca2b231acd524f3c` and 16/16 green checks.
- Merge and production commit: `8408fad6b872e000182e48be888293641c020367`.
- Backend is healthy with protocol `2024-11-05`, 114 tools and catalog `sha256:327a5ac4830172c9c64545c9b7d121487c773aed255f7c64e732606b491eaf99`.
- Stable facade is healthy on `front-door-stable`; its public temporary domain is unchanged.
- Durable coordinator journal remains target/state `idle`, revision 0, with no transition, recovery target or compensation.

## Demonstrated production failure

One coordinator reconciliation was dispatched from the exact reviewed preview:

- deployment `jigawl3rhnpmjmwb2eqo4j85`;
- commit `8408fad6b872e000182e48be888293641c020367`;
- terminal state `failed`;
- readiness remained HTTP 503;
- sanitized startup code `topology_front_application_transport_failed`;
- Coolify rolled back the unhealthy container.

The missing `curl` message is not causal: the healthcheck falls back to `wget`, which reached `/readyz` and received the intentional 503. The deployed binary cannot distinguish gateway resolution, address policy, refusal, timeout, route failure or another connection error.

## Active branch and scope

- Branch: `fix/front-door-coordinator-transport-diagnostics`.
- Base/upstream/origin main: `8408fad6b872e000182e48be888293641c020367`.
- Existing local work is preserved and limited to closed private-transport diagnostics.
- No host, IP, port, URL, token, response body or raw network error may enter logs, status, Brain or MCP output.

## Required sequence

1. Finish exact RED/GREEN regressions for closed transport classification and container smoke.
2. Run focused tests, complete suite, Vet, build and `git diff --check`.
3. Commit once, publish the branch and create a non-draft PR.
4. Require 16/16 exact-head checks green and merge by merge commit.
5. Synchronize clean `main`; deploy backend normally only if production does not already serve the merge.
6. Reconcile the same coordinator exactly once and observe the returned deployment only.
7. Use the resulting closed subcode to prove the real private transport cause.
8. Implement a separate minimal correction only after that proof, with its own PR/CI/merge.
9. Require coordinator deployment finished, application healthy, `/healthz` 200, `/readyz` 200 and journal idle/0 before continuity testing.
10. Complete real `front.*` MCP session/SSE/tool continuity proof before any cutover.
