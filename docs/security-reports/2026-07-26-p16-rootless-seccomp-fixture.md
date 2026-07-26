# P16 rootless BuildKit incident — 2026-07-26

## Cause

The VPS failure was caused by the systemd namespace restriction in `mcp-devbox-buildkit.service`. Denying IPC and UTS namespace creation prevented the OCI runtime child from starting and produced the otherwise opaque final-child PID error.

The approved correction changes only the namespace restriction from denying cgroup, IPC and UTS namespaces to denying only nested cgroup namespaces. All other service hardening, identity boundaries and CPU, memory, task and I/O controls remain unchanged.

AppArmor was ruled out by the VPS evidence and its existing path-scoped profile remains unchanged.

## CI gap

The former CI fixture used a scratch image with a file copy. BuildKit completed that graph internally, so it never exercised the OCI runtime and could not detect this failure.

The corrected fixture now executes a real `RUN` using the integrated Dockerfile frontend, repeats the build with cache import and requires cached evidence, then executes another real `RUN` through the external Dockerfile frontend. Each build verifies the resulting file content.

The VPS calibrator performs the same integrated and external runtime preflights before starting the six 50/65/80 measurements. A preflight failure is archived and stops the matrix.

## Deferred debt

`ProtectControlGroups=yes` and `Delegate=yes` have conflicting assumptions about cgroup filesystem writability. This did not cause the current incident because the rootless worker does not manage container cgroups. It must be resolved before that capability is enabled and is intentionally not changed here.
