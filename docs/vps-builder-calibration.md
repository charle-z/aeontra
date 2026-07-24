# P16 VPS rootless builder calibration

Status: **implemented as a closed Step 7 operator candidate; real VPS execution and dated baseline are still validation pending.**

This runbook governs the one-time 50/65/80 percent calibration of the private rootless BuildKit service. It does not register a public MCP tool, accept a command or URL, expose a Docker socket, change Coolify configuration, or select the final production quota without measured evidence.

## Preconditions

The host must already have the reviewed private candidate installed and healthy:

- `mcp-devbox-buildkit.service` is enabled and active;
- the service runs as the dedicated non-root `mcp-build` identity;
- the private socket is `/run/mcp-devbox-buildkit/buildkit/buildkitd.sock`;
- BuildKit binaries are the pinned, preverified candidate under `/usr/local/lib/mcp-devbox-builder`;
- cgroup v2 exposes CPU, memory, pressure and PID metrics for the complete service subtree;
- `https://mcp-devbox-charlez.duckdns.org/healthz` is the allowed control-plane health endpoint.

The script accepts exactly one value: a lowercase 40-character commit SHA from `charle-z/mcp-devbox`. It fetches only that commit from the fixed HTTPS repository. No branch, ref, repository, Dockerfile path, command, environment value, credential or destination is caller-controlled.

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
4. stages the pinned BuildKit release and checksum/SBOM/Sigstore evidence;
5. installs a new candidate, or reuses an existing candidate only when binaries,
   configuration and unit match byte-for-byte and the service is active;
6. runs the fixed calibrator for that same commit;
7. removes only a candidate created by that attempt when calibration fails;
8. preserves private calibration evidence and the preverified staging directory.

An existing different or partial installation fails closed and is never overwritten.
The bootstrap accepts no repository, URL, branch, command, credential, path or resource
limit from the caller.

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

The root-owned evidence directory is separate from the builder-writable source, output and cache roots. Builder-writable per-run directories are private and removed after calibration. Pending or failed evidence remains root-readable only.

## Failure and rollback

On success, failure, timeout or signal, the script:

1. terminates the full calibration process group;
2. restores `CPUQuota=65%` as the conservative candidate baseline;
3. records the exit status;
4. archives available evidence under `/var/lib/mcp-devbox-builder-calibration/`;
5. writes a separate SHA-256 file for the archive;
6. removes only the generated per-run work and cache directories.

It never removes the BuildKit service, shared service state, the dedicated user, repository data outside its fixed roots, or production application state.

## Evidence interpretation

A quota is not acceptable when any run has:

- a build failure or timeout;
- zero health samples;
- a non-200 health response or any HTTP 502;
- an OOM kill;
- missing artifact identity;
- invalid or unbounded cache evidence.

Among acceptable quotas, select the lowest quota that preserves reasonable no-cache and cached duration while keeping control-plane health stable. The decision must be recorded in a new dated baseline; the planning baseline from 2026-07-22 must not be rewritten.

The script does not automatically alter the systemd unit's persisted quota. After review, the selected value requires a separate tested configuration commit and deployment. Until then, the runtime rollback value remains 65 percent.

## Current evidence boundary

Repository and disposable GitHub Actions fixtures can prove packaging, rootless execution, cgroup membership, cache reuse and rollback mechanics. They cannot prove real VPS latency or resource contention. Step 8 must not begin until this script has run on the target VPS for an exact green commit and the dated evidence selects BuildKit and one quota, or records a structural stop honestly.
