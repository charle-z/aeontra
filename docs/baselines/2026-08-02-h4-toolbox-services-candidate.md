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

Source tests and CI are required before merge. Production deployment and a signed Edge
release remain separate facts. Real-device create/install/service/Edge-restart/
WSL-restart/repair/cleanup evidence must be recorded after the operator supplies the
next exact release number; it must not be inferred from `p15.0.12`.
