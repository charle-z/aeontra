# Tasks — P7 structured observability

Status: **active**.

- [x] **T01 P7 definition** — independent branch/spec and closed data boundary.
- [x] **T02 safe JSONL sink** — schema, concurrency, private bounded rotation.
- [x] **T03 immutable configuration** — flags/env/defaults/range validation.
- [x] **T04 MCP and HTTP events** — internal request ids and normalized completion data.
- [x] **T05 lifecycle events** — startup/shutdown identity without roots or private paths.
- [x] **T06 adversarial verification** — secret/body/path/target/raw-error non-disclosure.
- [x] **T07 operations documentation** — mounts, permissions, retention, update, rollback,
  and troubleshooting.
- [ ] **T08 P7 closure** — baseline, full gates, Actions, fast-forward, deployment, and
  exact production smoke.

## Boundary

P7 adds no public tool, remote exporter, dashboard, console, prompt capture, source
capture, user analytics, or new application.
