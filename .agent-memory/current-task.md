# Current task — Hito 4 persistent toolbox services

## Verified base and production

- Branch: `codex/toolbox-services-repair`.
- Base: `origin/main` at Hito 4 core merge
  `010c6091358f62b6fada35dbfc33eeaf50c2ae11`.
- PR #128 passed 16/16 exact-head checks and is merged/deployed.
- Backend serves 130 tools at
  `sha256:11697746d4c61b4035c8a6413b4ad63a0b29a50d343cf64d524640fdb719d03d`.
- Front Door deployment `f5lj9mh5l20zfnh8xjg3jvrg` finished, retired the old
  catalog, and remains healthy with a valid contract.
- Public OAuth discovery is 200 and unauthenticated `/mcp` is 401 with
  `resource_metadata`.
- Real Edge remains on signed `p15.0.12`; Hito 3A/3B/safe-sync/H4 real-device
  acceptance still requires one explicitly numbered signed release and update.

## Candidate scope

- Adds fail-closed repair plus named service start/status/stop on the same toolbox.
- Service identity is opaque and PID/start ticks remain private; no public path,
  process argv, environment, log or container identity is returned.
- Service status never starts a stopped toolbox. Commands are not persisted or replayed
  automatically after WSL/container restart.
- Candidate catalog is 134 tools at
  `sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`.

## Next exact action

PR #129 is open from `codex/toolbox-services-repair`; its initial exact head is
`59b2581a09c5cbf13e2d1cc84b9385e90b17296a`. Require exact-head CI, merge/deploy with a
bounded Front Door catalog transition and record Brain evidence. Do not infer an
immutable release number.

The first published documentation head `964cc6a3ae3fa0ef4a2524452a9aba18b7197b39`
failed only Staticcheck S1016 at `toolboxServiceSnapshot`. The mechanical typed
conversion is locally green under the exact Staticcheck v0.7.0; publication of that
correction is the active action.
