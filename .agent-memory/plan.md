# Plan — P12 Parrot onboarding hardening

1. Add regression coverage and code fixes for:
   - `bubblewrap_netlink_route_denied` classification;
   - bounded Bubblewrap verification diagnostics;
   - remote failure propagation for local preflight/journal rejection;
   - repeatable terminal objectives while preserving one active runtime per workspace;
   - WSL `/mnt/wsl/resolv.conf` acceptance without allowing `/mnt/c` or `/mnt/d`.
2. Package a P12 OpenCode Edge systemd unit with `AF_NETLINK`, non-root user execution, private state, and rootless workspace paths.
3. Add an executable Parrot onboarding preflight/smoke script and deterministic tests for the unit/script/docs contract.
4. Rewrite the Parrot installation/onboarding guide from the real successful procedure: exact merge, Node 24 wrappers, provider test, Bubblewrap + Podman preflight, pairing, workspace registration, service, unique smoke objective, diagnostics, rollback.
5. Update Linux Workcell baseline/status, README, documentation map, and Cubethon-facing presentation. Keep honest residual risks: host-shared network, rootless socket authority, no universal egress isolation.
6. Run focused tests, full Go suite, vet, diff checks, and documentation closure tests.
7. Commit in bounded steps, publish branch, open PR, and prepare the Cubethon issue draft. Do not merge or deploy without green checks.
