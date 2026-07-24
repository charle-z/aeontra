# Current task

Historical deployed baseline remains unchanged: the canonical VPS `main` worktree and production are synchronized separately; this P16 cut has not changed `main`, Coolify, production or Parrot.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head before this local fix: `e8aad0cba3933889f44ee08c57b3dc81f27f9cdb`, with 14/15 checks green. The sole failure was `Rootless Podman, PostgreSQL and Chromium`.

## Rootless fixture correction

The failure was isolated to the transition from `podman-compose down` to the PostgreSQL fixture. The fixture previously assumed Compose cleanup and rootless service readiness without proving either.

The local correction now:

- snapshots exact labeled container, pod, network and volume inventories before Compose;
- waits up to 15 seconds after `podman-compose down` for the inventory to converge exactly to its baseline;
- verifies the rootless Podman service is responsive before starting PostgreSQL;
- emits only closed PostgreSQL operation categories: `image_inspect`, `network_create`, `volume_create`, `container_run`, `readiness_query`;
- rejects arbitrary diagnostic categories;
- adds tests proving exact resource convergence and bounded diagnostic output.

Local validation is green:

- full `go test ./... -count=1`;
- tagged P12 rootless tests;
- `go vet ./...`;
- Staticcheck v0.7.0;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`.

Next: commit this Rootless fixture fix, publish to PR #48, hold the exact SHA stable, require all 15 checks green, diagnose any exact-head failure, then continue Step 6 admission and fairness test-first.
