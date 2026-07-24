# Private rootless BuildKit package candidate

Status: **not installed and not calibrated**.

This directory contains the private Step 7 packaging candidate for the dedicated `mcp-build` service. It is intentionally separate from the public control plane and is not wired into production deployment.

`install-preverified.sh` accepts no arguments and installs only root-owned, private, checksum-verified `buildkitd` and `buildctl` files from `/var/lib/mcp-devbox-builder-staging`. It snapshots prior managed files, rolls them back on failure, creates or validates the non-root builder account, requires subordinate UID/GID ranges, enables the service, and verifies the private Unix socket with `buildctl debug workers`.

`remove.sh` disables the service and removes only managed binaries, configuration and the unit. It preserves state, cache, the builder account and staging evidence. Symlinked managed paths fail closed.

This candidate must not be described as deployed until it has its own fixture tests, a reversible VPS preview, real cached/no-cache builds, 50/65/80 measurements, health latency and zero-502 evidence.
