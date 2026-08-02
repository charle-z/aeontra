# Current task — Hito 4 persistent toolbox core

## Verified base and production

- Branch: `codex/persistent-toolbox`.
- Base: `origin/main` at safe-sync merge
  `e7a2e6048c5a7a3a7fec77ee699653babaab244c`.
- PR #127 passed 16/16 exact-head checks and is merged/deployed.
- Backend serves 125 tools at
  `sha256:9f1ce2ece243c1d5e821adc9b037b21b50941125292485ac43748671d13451c8`.
- Front Door deployment `g13z5dohfncg8r16cv7w4ngn` finished, retired the old
  catalog, and remains healthy with a valid contract.
- Public OAuth discovery is 200 and unauthenticated `/mcp` is 401 with
  `resource_metadata`.
- Real Edge remains on signed `p15.0.12`; Hito 3A/3B/safe-sync real-device acceptance
  still requires one explicitly numbered signed release and update.

## Candidate scope

- One persistent rootless Debian toolbox is bound to one registered dev workspace.
- Private owner-only metadata records opaque toolbox identity, base image identity and
  timestamps; caller input cannot select images, paths, sockets, volumes or privileged
  flags.
- Create/status/arbitrary exec/install/explicit cleanup use the validated user-owned
  Podman/Docker endpoint. The project is mounted only at `/workspace`; the host package
  database is unchanged.
- Installed packages, toolchains, caches and writable rootfs have no TTL or automatic
  cleanup.
- Candidate catalog is 130 tools at
  `sha256:11697746d4c61b4035c8a6413b4ad63a0b29a50d343cf64d524640fdb719d03d`.

## Next exact action

Finish full package/documentation validation, review and commit the core slice, publish
one PR, require exact-head CI, merge/deploy with a bounded Front Door catalog
transition, then continue Hito 4 with service lifecycle, repair and background-process
integration. Do not infer an immutable release number.
