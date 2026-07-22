# VPS build CPU baseline — 2026-07-21

Status: pre-optimization evidence; post-merge verification pending.

## Host and deployment

- Production host: 2 vCPU, 4 GB RAM.
- Coolify application: `jqf7qz5ensoqtvl1tb197gcv`.
- Deployment: `q26b3h7iam9w25u01h93u8fq`.
- Source commit: `40c30d3b063c459fded577fec02322ef4595b586`.
- Started: `2026-07-22T03:28:13Z`.
- Finished: `2026-07-22T03:30:23Z`.
- Total deployment time: 130 seconds.

## Observed behavior

The hosting panel showed an idle CPU baseline near 20%, followed by a sustained build
spike that reached approximately 100% around the deployment window. The running MCP
remained healthy through most of the build, but one control-plane request returned a
transient HTTP 502 near the final container handoff. The deployment then completed
and the same runtime commit responded normally.

No OOM event, failed build, host restart, or persistent container failure was
observed. Exact RAM telemetry was not captured in this baseline, so it does not claim
that memory pressure was absent; the demonstrated bottleneck is CPU saturation.

## Change under test

The production Dockerfile now:

- runs only the console asset build instead of repeating CI checks and tests;
- defaults the console build to `BUILD_GOMAXPROCS=1` and
  `BUILD_UV_THREADPOOL_SIZE=1`;
- defaults the Go build to `BUILD_GOMAXPROCS=1` and
  `BUILD_GO_PARALLELISM=1`;
- uses persistent BuildKit caches for Go modules and compiled packages.

The trade-off is intentional: a cold build may take longer, but one logical CPU should
remain available for Coolify, Traefik, and the live application. Warm builds should
also avoid recompiling unchanged Go dependencies.

## Post-merge acceptance

Do not declare this optimization closed until a real production redeploy records:

1. all exact-head remote gates green;
2. successful Docker image build and healthy rollout;
3. no control-plane 502 during the build window;
4. a new hosting-panel CPU graph for the complete deployment window;
5. measured duration and peak CPU compared with this 130-second/~100% baseline.

A sustained peak materially below the previous saturation point is the goal. Brief
scheduler spikes are acceptable if the live control plane remains responsive.
