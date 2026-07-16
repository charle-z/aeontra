# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published validation SHA: `8051064a55d94a5d4e2bb8daa4d1c8248dc82e35`
Published validation tree: `91530f66205ec30a9540cb54b9a1b160d0201714`
Upstream: `origin/p11-2-remote-opencode-relay`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`
Draft PR: `https://github.com/charle-z/mcp-devbox/pull/13`

## Preserved deployed baselines

- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed; P11.2 does not alter either deployed release.

## First remote validation result

Run `29535155313` proved the host architecture on Ubuntu 22.04:

- Bubblewrap incremental preflight passed;
- Bubblewrap real isolation passed;
- combined remote relay under host Bubblewrap completed four turns/tools, distinct server/Edge/driver/OpenCode PIDs, request_ref, repository modification, tests and zero duplicates.

The host job failed only because it also invoked the network-none local benchmark on a host with a default route. The Docker job completed the local benchmarks but its test-only relay adapter failed before the first turn with `opencode_provider`.

## Pending second validation tree

Corrections now in the working tree:

- Docker relay and host combined reports use different filenames and explicit modes;
- all direct, relay, combined and isolation reports carry `git_tree`;
- host job depends on Docker, downloads fresh relay evidence from the same workflow and rejects tree mismatches;
- host combined step no longer invokes the Docker-only no-default-route benchmark;
- release candidate references relay, combined and isolation reports separately;
- Docker test-only adapter opens only `external_directory` after translating the virtual provider mount to a host path; production and host Bubblewrap remain deny, while Docker remains network-none/read-only with webfetch/websearch denied.

Local gates green after the correction:

- `go test -p 1 ./... -count=1`;
- `go test -tags=opencode_e2e -p 1 ./... -count=1`;
- Actionlint v1.7.12;
- `git diff --check`.

Next action: commit and publish this bounded CI correction, observe the new E2E run, then use only green report metrics for Parrot documentation and the P11.2 baseline.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
