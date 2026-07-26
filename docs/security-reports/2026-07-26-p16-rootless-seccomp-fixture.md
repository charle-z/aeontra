# P16 rootless BuildKit incident — 2026-07-26

## Cause

The VPS failure was caused by the systemd namespace restriction in `mcp-devbox-buildkit.service`. The direct host control proved IPC and UTS were blocked; the real CI `RUN` and the pinned BuildKit source then proved that cgroup namespace creation is also required on cgroup v2 hosts. Blocking any of those required namespaces prevented the OCI runtime child from starting and produced the otherwise opaque final-child PID error.

The first correction removed IPC and UTS from the deny-list, but the new real `RUN` fixture demonstrated that this remained insufficient. BuildKit v0.31.2 appends a cgroup namespace to the OCI specification when cgroup v2 namespace support is present. The final correction therefore removes only the `RestrictNamespaces` directive. All other service hardening, identity boundaries and CPU, memory, task and I/O controls remain unchanged.

AppArmor was ruled out by the VPS evidence and its existing path-scoped profile remains unchanged. GitHub's hosted Ubuntu 24.04 runner continued denying the nested rootless `/proc` mount even after its exposed AppArmor sysctls matched the confirmed VPS. The runc/seccomp fixture therefore runs on Ubuntu 22.04, where it can exercise the OCI runtime without weakening that unrelated hosted-runner policy. Ubuntu 24.04 acceptance remains mandatory on the real VPS through the two preflights and six-run calibration. Ubuntu 22.04 also lacks the newer `useradd --add-subids-for-system` option, so CI preseeds the reviewed builder account and fixed subordinate ID ranges and then exercises the installer's strict existing-account path.

## CI gap

The former CI fixture used a scratch image with a file copy. BuildKit completed that graph internally, so it never exercised the OCI runtime and could not detect this failure.

The corrected fixture now executes a real `RUN` using the integrated Dockerfile frontend, repeats the build with cache import and requires cached evidence, then executes another real `RUN` through the external Dockerfile frontend. Each build verifies the resulting file content.

The VPS calibrator performs the same integrated and external runtime preflights before starting the six 50/65/80 measurements. A preflight failure is archived and stops the matrix.

## Deferred debt

`ProtectControlGroups=yes` and `Delegate=yes` have conflicting assumptions about cgroup filesystem writability. This did not cause the current incident because the rootless worker does not manage container cgroups. It must be resolved before that capability is enabled and is intentionally not changed here.
