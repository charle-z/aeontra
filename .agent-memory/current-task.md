# Current task — Hito 4 resource controls

## Verified production base

- Branch base: `origin/main` at `f902fc4a229503a05eb47fa9ac4b3137b55d46f2`.
- PR #129 passed 16/16 exact-head checks, merged and auto-deployed.
- Backend serves 134 tools at
  `sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`.
- Front Door retirement deployment `vll165rrqplfulnw8oyh1ucs` finished with only the
  current catalog allowed. Public MCP challenge and OAuth discovery are healthy.
- Brain note `gpt-web-direct-edge-h4-toolbox-services` records the closure.

## Active source candidate

- Branch: `codex/toolbox-container-controls`.
- `project_toolbox_create` now accepts optional bounded CPU, memory and process limits.
- Defaults are 4000 millicores, 8192 MiB and 2048 processes.
- The limits persist in owner-only state, appear as safe public metadata and are
  revalidated against live rootless `HostConfig` on every operation.
- Reuse with different limits and live drift fail closed.
- The toolbox also receives the validated user-owned rootless engine at a fixed private
  path with server-owned client variables and reports bounded storage usage.
- Combined candidate catalog remains 134 tools at
  `sha256:9d8bea913bb9c0da9467dc0cfff414e02acd3893f1246f7a7e8e3d6a5a859236`.

## Next exact action

Compute and synchronize the catalog identity, run the complete gates and commit the
rootless-engine/storage slice separately. Then prove Compose/engine-native build and
decide the bounded BuildKit path before publishing Hito 4. Do not infer a signed Edge
release number after installed `p15.0.12`.
