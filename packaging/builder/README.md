# Private rootless BuildKit package candidate

Status: **not installed and not calibrated**.

This directory contains the private Step 7 packaging candidate for the dedicated `mcp-build` service. It is intentionally separate from the public control plane and is not wired into production deployment.

`bootstrap-vps.sh` is the fixed exact-commit Debian/Ubuntu host entrypoint. It reexecutes under a transient systemd unit, uses a fixed root PATH, installs `rootlesskit`, `uidmap`, `slirp4netns` and `fuse-overlayfs` when their reviewed binaries are missing, ensures the AppArmor parser is present only when the host has AppArmor enabled, and records private calibration evidence. It accepts no package, distribution, URL, path or command from the caller.

`install-preverified.sh` accepts no arguments and installs only root-owned, private, checksum-verified `buildkitd`, `buildctl` and `buildkit-runc` files from `/var/lib/mcp-devbox-builder-staging`. On AppArmor-enabled hosts it loads a path-scoped `userns` profile only for the reviewed private `buildkit-runc`; it never disables Ubuntu's system-wide unprivileged-user-namespace restriction. It snapshots prior managed files, rolls them back on failure, creates or validates the non-root builder account, requires subordinate UID/GID ranges, enables the service, and verifies the private Unix socket with `buildctl debug workers`.

`review-vps-calibration.sh` accepts one root-private evidence directory and validates the exact six-run matrix, cache reuse, health, OOM, PID, size and artifact-identity evidence. It deterministically selects the lowest eligible 50/65/80 quota within the reviewed duration thresholds, or records `none` and fails closed.

`remove.sh` disables the service and removes only managed binaries, configuration, the path-scoped AppArmor profile and the unit. It preserves state, cache, the builder account and staging evidence. Symlinked managed paths fail closed.

This candidate must not be described as deployed until it has its own fixture tests, a reversible VPS preview, real cached/no-cache builds, 50/65/80 measurements, health latency and zero-502 evidence.
