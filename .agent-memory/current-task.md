# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head before this local cut: `3bed257005c6b403fa95603eacab5784aa4090de`; 14 checks are green and the old head has two fixture failures: `Rootless BuildKit candidate fixture` and `Rootless Podman, PostgreSQL and Chromium`.

## P16 Step 7 local candidate

The exact logs from the existing VPS GitHub authority proved both failures were fixture defects rather than structural builder failures:

- BuildKit installed and ran rootless under the dedicated service cgroup, built the same commit twice and showed `CACHED`; only the final cache-size pipeline failed. The workflow now records a numeric `du -sb` cache byte count, enforces `0 < bytes <= 4 GiB`, and uploads `cache-bytes.txt`.
- PostgreSQL reached its healthcheck, but the immediate direct `psql` exec returned `not_found`. The fixture now uses a fixed `/bin/sh -ec` command, retries the bounded readiness query until the existing 60-second deadline, and emits the closed `postgres_readiness_query` category only after exhaustion.

`packaging/builder/calibrate-vps.sh` is now a closed root-only candidate for the real 50/65/80 percent matrix. It accepts one exact lowercase 40-character commit from the fixed owner repository, verifies the applied cgroup `cpu.max`, runs no-cache and cached builds with a 30-minute process-group timeout, samples the fixed `/healthz`, records CPU/throttling/memory/OOM/PSI/PID/cache/artifact identity and HTTP 502 evidence, bounds logs, archives private evidence and restores the conservative 65 percent quota on every exit.

The calibrator separates root-owned evidence from builder-writable source/output/cache roots. Git, curl and buildctl run with fixed empty environments; the host token, Coolify authority, proxy variables and Git global configuration are not inherited. It registers no public tool and has not run on the real VPS. `docs/vps-builder-calibration.md` is the source of truth; Step 8 remains blocked until exact-head CI and dated real-VPS evidence select an engine/quota or record a structural stop.

## Validation completed locally

Green on the exact local tree:

- focused BuildKit/package/rootless/docs tests;
- all Go packages in bounded groups;
- tagged `p12_e2e` compile gate;
- `go vet ./...`;
- Staticcheck v0.7.0, including the final builder package change;
- `go build ./...`;
- Actionlint v1.7.12 after workflow changes;
- `git diff --check`;
- no temporary helper/probe files remain.

Next: commit as `Step 7: Add bounded VPS builder calibration`, publish to PR #48, hold the SHA stable, require every exact-head gate green, diagnose any remaining fixture failure through the existing GitHub authority, then execute the real VPS calibration through a closed privileged path before beginning Step 8.
