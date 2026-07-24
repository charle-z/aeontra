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
- proof that rootlesskit, buildkitd, runtime helpers and compiler children remain under one delegated service cgroup subtree and UID;
- bounded ANSI/NUL/path/secret-redacted output;
- regular-file, no-symlink artifact verification with maximum size and SHA-256 identity;
- systemd properties that kill the complete control group on cancellation.

## Private candidate packaging

`packaging/builder/` contains an offline, fixed-input candidate package for the real
spike. It is not a public tool and is not installed by the MCP container.

- `mcp-devbox-buildkit.service` runs as the dedicated `mcp-build` user with the
  reviewed 65 percent candidate quota, memory/PID/I/O bounds, delegated cgroup and
  `KillMode=control-group`;
- `buildkitd.toml` is checked against the generated rootless configuration and fixes
  one OCI worker, one parallel build and bounded GC/cache thresholds;
- the service keeps `ProtectProc=invisible` but must not set `ProcSubset=pid` because
  BuildKit reads `/proc/stat` during a solve; cgroup and process-subtree isolation
  remain the enforcement boundary;
- `install-preverified.sh` accepts no arguments or URLs, consumes only a private
  root-owned staging directory containing `buildkitd`, `buildctl`, `buildkit-runc`
  and an exact three-entry SHA-256 manifest, creates the system identity with
  subuid/subgid ranges,
  activates the unit, verifies `debug workers`, and restores previous files if
  health fails;
- `remove.sh` disables the candidate and removes only its binaries, configuration and
  unit. State, cache, identity and preverified staging are preserved by default.

The candidate scripts remain inactive until a separately staged VPS spike supplies
preverified BuildKit binaries and records rollback evidence.

`stage-official-v0.31.2.sh` is the only networked preparation step. It accepts no
arguments, pins the official Linux amd64 release archive, SBOM and Sigstore bundle by
SHA-256, extracts only `buildkitd`, `buildctl` and `buildkit-runc`, and publishes a
private staging directory atomically without replacing different existing content.

`.github/workflows/p16-builder-spike.yml` exercises this package on Ubuntu 24.04:
it installs rootless prerequisites, stages v0.31.2, starts the systemd service, proves
all observed processes remain in the delegated subtree as `mcp-build`, builds the same
commit twice with cache reuse, stops the complete control group and verifies the
conservative removal path. This is disposable CI evidence, not VPS calibration.

## Remaining evidence

The following are deliberately not claimed yet:

- real rootless BuildKit installation and service lifecycle on the VPS;
- no-cache and cached builds for the same commit;
- 50/65/80 quota measurements;
- per-cgroup CPU throttling, PSI, memory, I/O and PID samples;
- control-plane health latency and observed 502 count;
- real cache reuse/GC behavior and uninstall execution on the VPS;
- final BuildKit-versus-Podman engine selection.

Those require the separate private spike deployment and dated measurement. If BuildKit fails a structural requirement, Podman may be evaluated without weakening the rootless/cgroup boundary.
