# P16 rootless BuildKit spike harness

Status: private Step 7 acceptance harness. It does not install a builder, execute a production build, register public MCP tools, or select the final quota.

## Boundary

The candidate engine is rootless BuildKit under a dedicated `mcp-build` identity. The reviewed service configuration accepts only the measured CPU candidates 50, 65 and 80 percent of one vCPU and fixes:

- runtime root `/run/mcp-devbox-buildkit`;
- state root `/var/lib/mcp-devbox-buildkit`;
- cache root `/var/cache/mcp-devbox-buildkit`;
- pinned builder binaries under `/usr/local/lib/mcp-devbox-builder`;
- `MemoryHigh=1280M`, `MemoryMax=1792M`, `TasksMax=512`, `IOWeight=25`;
- `KillMode=control-group` and cgroup-v2 process evidence.

No rootful Docker or generic BuildKit socket is accepted. Socket paths and every ancestor are checked without following symlinks, must remain inside the private runtime root, must be owned by the dedicated non-root UID, and must not be accessible by group or other users.

## Closed build plan

The harness creates one direct `buildctl` argv vector. It never accepts a command, shell fragment, arbitrary executable, free URL, privileged entitlement, host networking or rootful socket.

The context must be a private real directory under an approved workspace root. The Dockerfile name is fixed to `Dockerfile`. Output is an OCI archive under a separate private output root and is bound to an exact 40-character commit SHA. BuildKit local cache import/export is explicit under the private cache root.

## Verification helpers

The harness provides:

- exact cgroup-v2 parsing;
- proof that rootlesskit, buildkitd, runtime helpers and compiler children share one service cgroup and UID;
- bounded ANSI/NUL/path/secret-redacted output;
- regular-file, no-symlink artifact verification with maximum size and SHA-256 identity;
- systemd properties that kill the complete control group on cancellation.

## Remaining evidence

The following are deliberately not claimed yet:

- real rootless BuildKit installation and service lifecycle on the VPS;
- no-cache and cached builds for the same commit;
- 50/65/80 quota measurements;
- per-cgroup CPU throttling, PSI, memory, I/O and PID samples;
- control-plane health latency and observed 502 count;
- bounded cache cleanup and uninstall scripts;
- final BuildKit-versus-Podman engine selection.

Those require the separate private spike deployment and dated measurement. If BuildKit fails a structural requirement, Podman may be evaluated without weakening the rootless/cgroup boundary.
