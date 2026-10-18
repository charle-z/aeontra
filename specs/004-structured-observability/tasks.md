# Tasks — P7 structured observability

Status: **complete**.

- [x] **T01 P7 definition** — independent branch/spec and closed data boundary.
- [x] **T02 safe JSONL sink** — schema, concurrency, private bounded rotation.
- [x] **T03 immutable configuration** — flags/env/defaults/range validation.
- [x] **T04 MCP and HTTP events** — internal request ids and normalized completion data.
- [x] **T05 lifecycle events** — startup/shutdown identity without roots or private paths.
- [x] **T06 adversarial verification** — secret/body/path/target/raw-error non-disclosure.
- [x] **T07 operations documentation** — mounts, permissions, retention, update, rollback,
  and troubleshooting.
- [x] **T08 P7 closure** — `docs/baselines/2026-07-13-p7.md` records local gates,
  the initial Staticcheck U1000, corrective green Actions, automatic deployment, exact
  production identity, unchanged catalog, and inspected content-free JSONL events.

## Boundary

P7 adds no public tool, remote exporter, dashboard, console, prompt capture, source
capture, user analytics, or new application.
