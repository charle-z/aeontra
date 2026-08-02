# H4 persistent toolbox services candidate

Date: 2026-08-02

## Scope

- Adds `project_toolbox_repair` and
  `project_toolbox_service_start/status/stop` to the existing persistent toolbox.
- Reuses exactly one server-owned rootless container and its verified image, labels and
  single `/workspace` bind mount.
- Public service state is limited to opaque `ts_...` identity, closed name, lifecycle
  state and timestamps. PID, argv, environment, logs, paths and container identity stay
  private to the Edge.
- A fixed internal supervisor receives caller argv as positional values. Status and
  stop revalidate both PID and `/proc` start ticks; stop uses TERM, bounded grace and
  KILL only when required.
- Repair restarts only valid stopped/created state. It never recreates missing,
  unowned, unknown or unsafe state.
- Service argv is not persisted or automatically replayed. Container/WSL restart makes
  the service durably observable as stopped; a new explicit start receives a new opaque
  identity.

## Candidate identity

- Public catalog: 134 tools.
- Catalog hash:
  `sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`.

## Acceptance posture

PR #129 is the publication candidate. Initial source head:
`59b2581a09c5cbf13e2d1cc84b9385e90b17296a`.
The first documentation follow-up head
`964cc6a3ae3fa0ef4a2524452a9aba18b7197b39` exposed Staticcheck S1016 in one snapshot
literal; the equivalent typed conversion passed Staticcheck v0.7.0 locally and is the
only functional-source correction after that run.

Local source validation is green:

- focused Edge/manager/MCP/documentation tests;
- ordinary full suite in a non-root Linux filesystem with Node 22;
- atomic coverage and the repository coverage gate (`internal/mcpserver` 80.7%);
- `go vet ./...`, `go build ./...`, and `git diff --check`;
- full race execution except one pre-existing wall-clock detector exceeded its limit
  under the concurrent run, then passed alone under `-race` in 1.681 seconds.

Exact-head CI remains authoritative before merge. Production deployment and a signed
Edge release remain separate facts. Real-device create/install/service/Edge-restart/
WSL-restart/repair/cleanup evidence must be recorded after the operator supplies the
next exact release number; it must not be inferred from `p15.0.12`.
