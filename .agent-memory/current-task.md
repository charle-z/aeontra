# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`.

Published head before this documentation-only correction: `0aaeca2c469bab96f71d31a760f6f7a3cb6b20d2`.

## Exact-head evidence

`Rootless BuildKit candidate fixture` is green on `0aaeca2`. The exact job proves:

- the private rootless service started under `mcp-build`;
- the complete rootlesskit/buildkitd process subtree remained in the reviewed cgroup;
- both OCI builds completed;
- the second build emitted `CACHED`;
- artifact verification, `buildctl du -v`, the 4 GiB cache policy, stop behavior and conservative removal passed.

`Verify` failed only in documentation closure tests because the previous current-task rewrite omitted historical P8.1/P9 markers. No source, runtime or workflow test failed.

## Current correction

This file restores the mandatory deployed-history statements for:

- P8.1 at `d343264bffdc0ae1bc045a9d723e913be977090c`;
- P9 Brain as its deployed successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

No production, Coolify, Parrot, `main`, security boundary or builder behavior changes in this correction.

## Step 7 state

The branch contains:

- the private rootless BuildKit package and disposable CI fixture;
- cache reuse and bounded cache-policy evidence;
- the fixed 50/65/80 VPS calibrator;
- a durable exact-commit root bootstrap that survives SSH disconnects, inherits no credentials, rejects different/partial installations and preserves private evidence.

Next: publish this documentation-only correction, hold the exact SHA stable, require every PR check green, then merge only through the reviewed green PR path. Real VPS calibration remains a host-root boundary and must use the committed bootstrap for the exact green commit before Step 8.
