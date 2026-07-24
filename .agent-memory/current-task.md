# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head before this local cut: `03ba41ded614f223c762e6071d50055df2644371`.

## Exact-head evidence

The previous published head again proved the full rootless BuildKit lifecycle, two successful OCI builds and a `CACHED` second solve. It failed before the Python parser because `sudo du -sb` could not traverse the cache created by the rootless `mcp-build` identity. The upload contained no cache evidence file, confirming the failure occurred at the root-side traversal rather than at the numeric bound.

`Rootless Podman, PostgreSQL and Chromium` is green. All other checks on the prior head were green or progressing normally; the BuildKit engine itself has not failed a solve.

## Local follow-up ready to publish

The workflow now inventories the local cache as `mcp-build`, the same identity that created it. A fixed Python traversal:

- starts from the fixed cache root;
- uses `follow_symlinks=False`;
- rejects symlinks and special files;
- rejects filesystem-boundary changes;
- caps the inventory at 10,000 entries;
- enforces total regular-file bytes between 1 and 4,294,967,296;
- writes only `entries=<n>` and `bytes=<n>` to `cache-inventory.txt`.

Workflow/docs tests, Actionlint v1.7.12 and `git diff --check` are green. No temporary helper or diagnostic file remains.

The closed real-VPS calibrator from `64b6fe9` remains unchanged and unexecuted. The deployed MCP runs as non-root UID 10001 inside an Alpine container with no host systemd or host filesystem authority; real builder installation/calibration therefore cannot be executed by the current public MCP tool surface. After exact-head CI is green, the remaining human boundary must be reduced to one reviewed root bootstrap on the VPS, or a separately installed administrator runner. Step 8 remains blocked until dated real-host evidence selects the engine/quota.
