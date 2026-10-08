# P16 VPS rootless builder calibration

Status: **target-VPS rootless runtime preflight accepted; the dated six-run 50/65/80 quota baseline remains validation pending.**

This runbook governs the one-time 50/65/80 percent calibration of the private rootless BuildKit service. It does not register a public MCP tool, accept a command or URL, expose a Docker socket, change Coolify configuration, or select the final production quota without measured evidence.

## Preconditions

The host must already have the reviewed private candidate installed and healthy:

- `mcp-devbox-buildkit.service` is enabled and active;
- the service runs as the dedicated non-root `mcp-build` identity;
- the private socket is `/run/mcp-devbox-buildkit/buildkit/buildkitd.sock`;
- BuildKit binaries are the pinned, preverified candidate under `/usr/local/lib/mcp-devbox-builder`;
- cgroup v2 exposes CPU, memory, pressure and PID metrics for the complete service subtree;
- `https://mcp-devbox-charlez.duckdns.org/healthz` is the allowed control-plane health endpoint.

The script accepts exactly one value: a lowercase 40-character commit SHA from `charle-z/aeontra`. It fetches only that commit from the fixed HTTPS repository. No branch, ref, repository, Dockerfile path, command, environment value, credential or destination is caller-controlled.

## Reviewed invocation

After the candidate script and service have been installed through the reviewed release path, the sole operator action is:

```bash
sudo /usr/share/mcp-devbox-builder-candidate/calibrate-vps.sh <EXACT_40_CHARACTER_COMMIT>
```

This is intentionally not a free shell profile. A future MCP wrapper, if added, must bind the exact commit and invoke only this fixed script.

## Durable host bootstrap

The deployed MCP container cannot perform this host mutation: it runs as non-root UID
10001 inside an Alpine container, has no host systemd and intentionally has no host
filesystem or Docker socket. Installing the rootless builder is therefore a real host
administrator boundary, not something that should be hidden behind the public MCP.

`packaging/builder/bootstrap-vps.sh` reduces that boundary to one reviewed root
invocation with the exact green commit:

```bash
sudo /root/mcp-devbox-builder-bootstrap.sh <EXACT_40_CHARACTER_COMMIT>
```

The host bootstrap supports only Debian or Ubuntu with systemd and cgroup v2. It uses a fixed root PATH and, when needed, installs only `rootlesskit`, `uidmap`, `slirp4netns` and `fuse-overlayfs` through non-interactive APT. Package names and the operating-system allowlist are not caller-controlled.

The downloaded entrypoint must be a regular, executable, root-owned file that is not
writable by group or other. The bootstrap reexecutes itself as the fixed transient
`mcp-devbox-builder-bootstrap.service` unit with a four-hour maximum runtime.
The work survives an SSH disconnect and its progress is available through:

```bash
systemctl status mcp-devbox-builder-bootstrap.service --no-pager
journalctl -u mcp-devbox-builder-bootstrap.service --no-pager
```

Inside the transient unit, the bootstrap:

1. takes a private host lock;
2. fetches only the exact commit from the fixed public owner repository using an empty
   Git environment and no credential helper;
3. verifies a clean detached checkout and the fixed scripts' ownership/mode;
4. verifies the Debian/Ubuntu host and installs the four fixed rootless packages only when their reviewed binaries are missing;
5. stages the pinned BuildKit release and checksum/SBOM/Sigstore evidence;
6. installs a new candidate, or reuses an existing candidate only when binaries,
   configuration and unit match byte-for-byte and the service is active;
7. runs the fixed calibrator for that same commit and records exact prerequisite package versions in `host-prerequisites.tsv`;
8. removes only a candidate created by that attempt when calibration fails;
9. preserves private calibration evidence, installed host prerequisites and the preverified staging directory.

An existing different or partial installation fails closed and is never overwritten.
The bootstrap accepts no repository, URL, branch, command, credential, path or resource
limit from the caller.

## Validation before measurement

Before any quota measurement, the calibrator runs two fixed preflights at the conservative
65 percent quota. The integrated Dockerfile frontend preflight builds `busybox:1.37.0`
with a `RUN` instruction and verifies `/ok`. Only after that succeeds, the external Dockerfile frontend preflight
uses `docker/dockerfile:1.7` for its own `RUN` and output verification. A
preflight failure archives its bounded log and prevents all six measurements from
starting. This order isolates the OCI runc/namespace path before testing the external
frontend container.

The evidence includes `preflight-status.tsv`, both preflight logs, the effective
`RestrictNamespaces`, `ProtectKernelTunables`, `ProtectKernelModules` and
`ProtectHostname` properties, and `confirmed-incident.txt`. The incident record treats
the namespace filter and the two procfs-obscuring systemd options as one rootless
container incident, records that `ProtectKernelModules=yes` remains safe and enabled,
notes that AppArmor was discarded with direct host evidence, and preserves the former
`FROM scratch` plus `COPY` CI gap.

## Measurement sequence

The calibrator serializes itself with a private lock and runs six builds against the same exact commit:

```text
50%: no-cache, cached
65%: no-cache, cached
80%: no-cache, cached
```

For each run it:

- applies the requested CPU quota and verifies the exact value from cgroup `cpu.max`;
- executes one bounded BuildKit solve with a 30-minute hard timeout;
- captures cgroup CPU usage and throttling, memory peak/high/OOM events, CPU/I/O pressure and PID peak;
- samples `/healthz` during the build and records latency, non-200 responses and HTTP 502 responses;
- records cache bytes plus OCI artifact size and SHA-256 identity;
- bounds retained build output to 32 MiB;
- removes the temporary OCI archive after recording its identity.

After all six measurements, the fixed `review-vps-calibration.sh` selector validates
the exact matrix and writes three root-private files into the evidence directory:

- `selection.tsv`, with hard and duration eligibility for every quota;
- `selected-quota-percent`, containing one of `50`, `65`, `80`, or `none`;
- `selection-policy`, recording the deterministic reviewed threshold.

A quota is hard-eligible only when both modes complete within the existing 30-minute
timeout, prove cache reuse from the bounded cached-build log, produce bounded cache
and artifact evidence, record a valid SHA-256 identity, observe at least one health sample, and report zero build failures, OOM kills,
non-200 health responses, and HTTP 502 responses. Among hard-eligible quotas, the
selector chooses the lowest quota whose no-cache duration is at most 135 percent and
whose cached duration is at most 125 percent of the fastest hard-eligible run in the
same mode. An incomplete, duplicate, malformed, oversized, or internally inconsistent
matrix fails closed. If no quota satisfies the policy, the evidence is archived as a
structural stop and Step 8 remains blocked.

The root-owned evidence directory is separate from the builder-writable source, output and cache roots. Builder-writable per-run directories are private and removed after calibration. Pending or failed evidence remains root-readable only.

## Failure and rollback

On success, failure, timeout or signal, the script:

1. terminates the full calibration process group;
2. restores `CPUQuota=65%` as the conservative candidate baseline;
3. records the exit status;
4. archives available evidence under `/var/lib/mcp-devbox-builder-calibration/`;
5. writes a separate SHA-256 file for the archive;
6. removes only the generated per-run work and cache directories.

It never removes the BuildKit service, shared service state, the dedicated user, fixed host prerequisite packages, repository data outside its fixed roots, or production application state.

## Evidence interpretation

A quota is not acceptable when any run has:

- a build failure or timeout;
- zero health samples;
- a non-200 health response or any HTTP 502;
- an OOM kill;
- missing artifact identity;
- invalid or unbounded cache evidence.

Among acceptable quotas, use the deterministic selector result described above. The
decision must still be reviewed against the raw evidence and recorded in a new dated
baseline; the planning baseline from 2026-07-22 must not be rewritten.

The script does not automatically alter the systemd unit's persisted quota. After review, the selected value requires a separate tested configuration commit and deployment. Until then, the runtime rollback value remains 65 percent.

A documented hardening debt remains intentionally unchanged: `ProtectControlGroups=yes` and `Delegate=yes`
have conflicting assumptions about cgroup filesystem writability.
This does not cause the current failure because the rootless OCI worker does not manage
container cgroups, but it must be revisited before enabling that capability.

## Current evidence boundary

Repository and disposable GitHub Actions fixtures can prove packaging, AppArmor profile loading, cgroup membership and rollback mechanics. The hosted runner's OCI execution path is explicitly not reproducible because its host policy denies the nested rootless `/proc` mount. Step 8 must not begin until this script has run on the target VPS for an exact green commit and the dated evidence proves both runtime preflights, completes the six-run matrix and selects BuildKit and one quota, or records a structural stop honestly.
