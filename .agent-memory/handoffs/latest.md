# Handoff — Stable MCP Front Door private transport diagnostics

The Stable MCP Front Door Brain gate remains open. Functional Edge roadmap work,
cutover and rollback are blocked until the private coordinator is healthy and real
MCP continuity is proven.

Verified production base:

- repository and backend commit `8408fad6b872e000182e48be888293641c020367`;
- backend protocol `2024-11-05`, 114 tools and catalog
  `sha256:327a5ac4830172c9c64545c9b7d121487c773aed255f7c64e732606b491eaf99`;
- healthy managed facade `o338wpoy1254d83ud2y8p1v8`;
- private coordinator `v13i2apwnvu09ms7l77x6opk`, with no public domain;
- durable journal target/state `idle`, revision 0 and no dispatched transition.

One reviewed coordinator reconciliation produced deployment
`jigawl3rhnpmjmwb2eqo4j85`. It built and started, but `/readyz` remained 503,
emitted only `topology_front_application_transport_failed`, became unhealthy and
was rolled back. The healthcheck fallback reached the service with `wget`; absence
of `curl` was not the cause.

Active branch: `fix/front-door-coordinator-transport-diagnostics`.

The branch preserves only closed private-transport classes: fixed target,
resolution, address policy, refusal, timeout, route and generic connection failure.
Raw network errors, origin, host, IP, port, token and response bodies remain absent
from status, logs and MCP results. Unit and runtime mappings, container smoke and
the stable-front-door runbook are synchronized.

Verification completed:

- focused coordinator and workflow-policy tests;
- all `cmd`, `docs`, integrations, packaging and profile tests;
- all internal package tests in deterministic batches;
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`.

Next exact sequence:

1. Commit and publish this diagnostic-only branch.
2. Open a non-draft PR and require all exact-head checks green.
3. Merge by merge commit and verify production serves the merge.
4. Reconcile the same private coordinator exactly once and observe that deployment.
5. Use its closed subcode as proof for a separate minimal correction PR.
6. Require coordinator health, ready journal and real `front.*` MCP continuity
   before any cutover.
