# Current task

Historical deployed baseline remains unchanged: this P16 branch has not changed `main`, Coolify, production or real Parrot.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Step 6 head `37c02f0067d7aa0023bd87f204a37a6ab91f1ec8` passed 15/15 exact-head checks.

## P16 Step 7 private BuildKit spike harness

A private, non-production acceptance harness now exists under `internal/buildspike`. It adds no public MCP tools and performs no production installation or build.

Implemented and tested:

- dedicated non-root `mcp-build` identity and fixed private runtime/state/cache roots;
- only measured CPU candidates 50/65/80 percent of one vCPU;
- fixed memory, PID and I/O candidate bounds;
- rootful Docker/generic BuildKit socket rejection;
- symlink, ownership, mode and root escape rejection for rootless socket paths;
- closed direct `buildctl` argv with no shell, sudo, privileged entitlement or host network;
- exact 40-character commit binding and private workspace/output roots;
- reusable bounded local cache import/export and rootless BuildKit config;
- cgroup-v2 parsing and process evidence proving rootlesskit/buildkitd/helpers/compiler children share one UID and service cgroup;
- process-group cancellation that kills children before the leader to avoid surviving/zombie children;
- bounded closed environment and output capture with ANSI/NUL/path/secret redaction;
- bounded regular-file OCI artifact identity using SHA-256;
- cgroup CPU, throttling, memory, PSI and event parsing with malformed/oversized rejection;
- blocking package coverage threshold: `internal/buildspike` >=75%, measured 82.0%.

Documentation:

- `docs/buildkit-spike-harness.md`;
- `docs/testing.md` and `docs/quality-gates.md`;
- Step 7 checklist marks only locally proven controls complete.

Not yet claimed:

- real rootless BuildKit installation/service lifecycle on the VPS;
- real cached/no-cache build of the same commit;
- 50/65/80 calibration and dated cgroup/health evidence;
- zero observed 502 proof;
- final engine/quota selection;
- production deployment or cleanup/uninstall evidence.

Next: run final full local gates, commit `Step 7: Add rootless BuildKit spike harness`, publish to PR #48, hold the exact SHA until all checks are terminal, then continue with the private real spike packaging/calibration plan without touching production until an explicitly staged and reversible action is ready.
