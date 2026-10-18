# P16 rootless BuildKit incident — 2026-07-26

## Cause

The VPS failure was one incident with three confirmed systemd hardening blockers in `mcp-devbox-buildkit.service`. First, `RestrictNamespaces` installed seccomp filters that denied IPC, UTS and cgroup namespaces required by the BuildKit v0.31.2 OCI specification on cgroup v2 hosts. That killed the OCI runtime child before it could report its PID.

After the namespace filter was removed, the target-host preflight reached the real BusyBox `RUN` and exposed the remaining nested-procfs failure. `ProtectKernelTunables=yes` made `/proc/sys` read-only and blocked the mount by itself. `ProtectHostname=yes` introduced a UTS namespace and became incompatible when combined with the retained `ProtectSystem=strict` mount posture. Removing those two options allowed `unshare --mount-proc` and the real BuildKit preflight to complete.

The final service therefore omits exactly `RestrictNamespaces`, `ProtectKernelTunables` and `ProtectHostname`. `ProtectKernelModules=yes` was tested separately and remains enabled, together with the remaining identity, filesystem, process, address-family, CPU, memory, task and I/O controls. The practical risk of removing the two procfs-related options is negligible because the service runs as the non-root `mcp-build` identity with no ambient capabilities; changing initial-namespace tunables or the host name still requires initial-namespace `CAP_SYS_ADMIN`.

AppArmor was ruled out by the VPS evidence and its existing path-scoped profile remains unchanged. The GitHub-hosted Ubuntu 24.04 runner has a different host policy and denies runc's nested rootless `/proc` mount. CI records only that exact denial as `not-reproducible`; it does not change AppArmor, the operating system or the runner security posture. The target VPS is the runtime acceptance environment.

## CI gap

The former CI fixture used a scratch image with a file copy. BuildKit completed that graph internally, so it never exercised the OCI runtime and could not detect this failure.

The corrected Ubuntu 24.04 fixture attempts a real integrated `RUN`. When the hosted runner returns its reviewed nested `/proc` mount denial, the workflow writes an explicit `not-reproducible` classification and remains non-blocking; any other failure is blocking. When the runner can execute runc, the fixture also requires cached evidence and validates the external frontend output.

The VPS calibrator performs the integrated and external runtime preflights before starting the six 50/65/80 measurements. A preflight failure is archived and stops the matrix. This target-host run is the only runtime acceptance gate.

## Deferred debt

`ProtectControlGroups=yes` and `Delegate=yes` have conflicting assumptions about cgroup filesystem writability. This did not cause the current incident because the rootless worker does not manage container cgroups. It must be resolved before that capability is enabled and is intentionally not changed here.

## Abandoned runner-parity path

PR #55 originally accumulated ten commits while trying to make a disposable GitHub
runner behave like the target VPS. The clean reconstruction retains the final product
fix and the useful intent of the real-runc fixture, and supersedes eight dead-end commits
in the final tree:

- 6d13aba: removed only IPC and UTS from the namespace deny-list; this was incomplete
  because BuildKit v0.31.2 also requests a cgroup namespace on cgroup v2 hosts;
- 7bd0477: adjusted CI to verify systemd's normalized namespace representation;
- 43dfbf4: added bounded CI failure annotations while still treating the hosted runner
  as a production-equivalent runtime;
- e44d6ff: changed hosted-runner AppArmor sysctl posture to resemble the VPS;
- 98ebb23: moved the runtime fixture to Ubuntu 22.04;
- 32c53f7: instrumented Ubuntu 22.04 installer failures;
- 9bbc9eb: seeded an Ubuntu 22.04 identity to bypass its older useradd behavior;
- 757911e: split package validation and runtime execution across Ubuntu 24.04 and
  Ubuntu 22.04 jobs.

These attempts were abandoned because changing the runner OS, AppArmor ABI, identity
setup or security posture can produce a green result without reproducing production.
The hosted Ubuntu 24.04 environment denies a nested rootless /proc mount that the VPS
permits, while Ubuntu 22.04 cannot load the reviewed AppArmor 4.0 profile. Neither is an
honest substitute for the target host.

The repository rule is now explicit: when kernel, systemd, AppArmor or namespace
behavior cannot be reproduced faithfully in CI, the workflow records the concrete
reason as not-reproducible, unexpected failures remain blocking, and runtime
acceptance moves to the target host. It is forbidden to change the runner operating
system, AppArmor version or security posture merely to obtain a green gate.

The repository MCP forbids force-push and exposes no reviewed ref-update profile, even
when a one-time exception is authorized externally. PR #55 is therefore published with
a non-force merge bridge: the final tree is the single clean reconstruction, while the
discarded diagnostic commits remain visible only in ancestry. They are not accepted as
product evidence and must not be copied into future work.
