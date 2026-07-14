# P9 Brain — release candidate closure

Status: P9 is complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`. Closure commit `b89236b` records the dated baseline and exact prior-SHA evidence; the verified follow-up correction separates release-candidate verification from still-pending production closure. Production remains P8/62 until fresh checks pass, PR #4 merges, `/brain` persistence is configured, and the merged application is deployed and smoked.

## Verified

- P9 candidate identity: 67 tools and `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`;
- original 62 P8 contracts remain the exact prefix with historical hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- P9 preserves the no resident service invariant;
- exact implementation head `96f7ca15183271772aecbf2d0ac2cceb88e20e5d` passed CI run `29306099092` and Security Evidence run `29306099088`;
- closure commit and correction tree passed formatting, full tests, atomic coverage thresholds, vet, build, Actionlint, Govulncheck, both Brain fuzz targets, Staticcheck with temporary writable caches, and `git diff --check`;
- no runtime, console, OAuth, Edge, workcell or HTB implementation was added by the closure work.

## Next exact actions

1. Commit the verified release-state correction on `p9-brain`.
2. Publish the branch without force.
3. Require fresh PR checks for the final SHA; do not reuse checks from `96f7ca1` as final closure evidence.
4. Merge PR #4 only after every required job succeeds.
5. Inspect the existing Coolify app `jqf7qz5ensoqtvl1tb197gcv`; configure a persistent `/brain` mount and `MCP_DEVBOX_BRAIN_ROOT=/brain` while preserving `/repos` and `/state`.
6. Deploy once, observe the same deployment to terminal state, verify exact commit/health/67 tools/hash/logs/Brain readiness, and run `mcp-catalog-smoke` plus `brain-smoke` without private output.
7. Record deployed evidence and create the annotated `p9` tag only after production verification.
8. Only then audit the Opus/Fable frontend proposal and create an independent BIOS Operations Console branch/spec.
