# Hito 4 toolbox resource controls candidate — 2026-08-02

This baseline records source-candidate evidence. It is not a signed release or
real-device acceptance record.

## Base

- `origin/main`: `f902fc4a229503a05eb47fa9ac4b3137b55d46f2`.
- PR #129 is merged and production serves 134 tools at catalog
  `sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`.
- Stable Front Door retirement deployment: `vll165rrqplfulnw8oyh1ucs`.

## Candidate

- Branch: `codex/toolbox-container-controls`.
- Tool count remains 134; the schema change produces catalog
  `sha256:14de29c2c2c7dca8ba6d0621f57495940f88975d4fea0bd97a72f91848b03b84`.
- `project_toolbox_create` accepts optional CPU millicores, memory MiB and process
  count limits. Omitted values become 4000, 8192 and 2048 respectively.
- The private record binds the selected limits to the persistent container. Reuse with
  different limits is rejected, and every operation revalidates live Podman/Docker
  `HostConfig` values before acting.
- No socket, host path, container identity, argv, environment or secret is added to the
  public result. Safe applied limits are returned as metadata.

## Verification

- RED tests failed on the absent request/result fields and schemas.
- Focused contract, MCP, Edge-client and `mcp-edge` tests pass.
- A real rootless Podman acceptance container reported exactly:
  - memory `8589934592` bytes;
  - `NanoCpus=4000000000`;
  - `PidsLimit=2048`.
- The temporary acceptance container was removed.
- The ordinary suite is green except when run directly from DrvFS, where one existing
  permission-contract test sees synthetic mode `0777`; that same test passes from a
  Linux-filesystem copy.

## Remaining Hito 4 acceptance

- publish and deploy this candidate through a reviewed PR;
- implement direct rootless container/Compose/BuildKit lifecycle and bounded storage
  metadata/cleanup without exposing a privileged socket;
- publish an explicitly numbered signed Edge release;
- update the real Edge and prove restart persistence, network, toolchain installation,
  build, service and explicit cleanup.
