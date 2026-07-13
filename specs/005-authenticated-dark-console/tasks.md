# Tasks — P8 authenticated dark console

Status: **active**.

- [x] **T01 P8 definition** — independent branch/spec, presentation-only boundary,
  authentication/session contract, and no new application.
- [x] **T02 opaque session store** — digest-only ids, TTL, cap, revocation, concurrency.
- [x] **T03 embedded dark UI** — semantic dependency-free HTML/CSS/JS and reduced motion.
- [x] **T04 authenticated routes** — login/logout/page/status/assets in the existing mux.
- [x] **T05 browser hardening** — CSP, frame/referrer/content-type/permissions/cache,
  strict methods, and bounded bodies.
- [x] **T06 adversarial tests** — token/session/content/path/target leakage and auth bypass.
- [x] **T07 operations documentation** — setup, HTTPS cookie behavior, rollback,
  troubleshooting, and limitations.
- [ ] **T08 P8 closure** — baseline, full gates, Actions, fast-forward, deployment, and
  authenticated production validation.

## Boundary

P8 cannot execute MCP tools, approve plans, enumerate private resources, expose
observability/audit history, mutate policy/configuration, or create another application.
