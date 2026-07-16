# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published HEAD before validation commit: `597f668859a5d45263d301fdcecfc0335ecde2b3`
Upstream: `origin/p11-2-remote-opencode-relay`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`
Draft PR: `https://github.com/charle-z/mcp-devbox/pull/13`

## Preserved deployed baselines

- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed; P11.2 does not alter either deployed release.

## Pre-CI validation checkpoint

The tree now separates three explicit modes:

1. `relay_container_e2e`: unprivileged Docker relay/OpenCode/provider validation;
2. `bubblewrap_host_e2e`: incremental preflight and isolation directly on Ubuntu 22.04;
3. `combined_opencode_sandbox_e2e`: server, Edge, driver and OpenCode under host Bubblewrap with distinct PIDs.

Implemented:
- closed, redacted Bubblewrap stage classification;
- host preflight for user namespace, UID/GID maps, mounts, binds, Unix socket, unshare-all, blocked network/DNS and helper execution;
- host isolation report with runner/tree/versions, read-only and visibility invariants, modes and startup samples;
- fail-closed launcher code and negative test;
- Docker test-only execution path behind the `opencode_e2e` build tag; production binaries never compile it;
- removal of `SYS_ADMIN`, AppArmor unconfined and seccomp unconfined from Docker;
- checkout/setup-go/setup-node/upload-artifact v6 pinned by verified full SHA;
- Actions Node remains 24; OpenCode remains 1.18.1.

Local gates green before publishing:
- standard and tagged full tests;
- vet;
- build;
- diff check;
- Actionlint v1.7.12.

Next action: commit and publish the validation tree, then observe GitHub Actions acutely. Do not wait indefinitely; checkpoint after the run completes or after a bounded observation window.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
