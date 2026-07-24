# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head: `64b6fe96975a753e43c7fc45b89efc3096a5dd5a`.

## Exact-head evidence

The published head completed 15/16 checks green. `Rootless Podman, PostgreSQL and Chromium` is now green, confirming the bounded PostgreSQL readiness retry. The only red check is `Rootless BuildKit candidate fixture`.

The exact BuildKit log proves:

- pinned rootless BuildKit installed and became healthy;
- rootlesskit/buildkitd/slirp4netns remained under the dedicated non-root service cgroup;
- both OCI builds succeeded;
- the second solve emitted `CACHED`;
- failure occurred only after both builds, inside the cache-byte evidence check.

## Local follow-up ready to publish

The workflow no longer relies on shell integer parsing for `du`. It stores the raw `sudo du -sb` line in `cache-du.txt`, parses it with a strict Python full match, enforces `0 < bytes <= 4_294_967_296`, writes the normalized value to `cache-bytes.txt`, and uploads both artifacts. Actionlint v1.7.12, workflow/docs tests and `git diff --check` are green.

The closed `packaging/builder/calibrate-vps.sh` candidate remains unchanged from `64b6fe9`: exact commit and fixed repository/health endpoint, 50/65/80 no-cache+cached matrix, exact `cpu.max` verification, bounded process group, cgroup/health/502/cache/artifact evidence, fixed empty environments, private archive and rollback to 65 percent. It has not run on the real VPS.

Next: commit the cache-evidence parser fix, publish one stable head, require all exact-head gates green, then establish the only safe path for real VPS installation/calibration. Step 8 remains blocked until a dated real-host baseline selects the engine/quota or records a structural stop.
