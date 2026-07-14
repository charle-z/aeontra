# P9 Brain — release candidate closure

Status: P9 is complete / merge-ready on branch `p9-brain`. The reviewed implementation head is `96f7ca15183271772aecbf2d0ac2cceb88e20e5d`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`. Production still serves P8/62 until PR #4 is merged, the existing Coolify application receives the dedicated persistent `/brain` volume plus `MCP_DEVBOX_BRAIN_ROOT=/brain`, and the merged commit is deployed and smoked.

## Verified

- branch clean and synchronized with `origin/p9-brain` before closure edits;
- P9 catalog candidate: 67 tools and `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`;
- original 62 P8 contracts remain the exact prefix and preserve historical hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`;
- P9 preserves the no resident service invariant: SQLite is in-process and no new application, service, port, worker, queue, model or database server is introduced;
- PR #4 is open, non-draft and mergeable at exact head `96f7ca15183271772aecbf2d0ac2cceb88e20e5d`;
- CI run `29306099092`: Verify, Race detector, Staticcheck and Govulncheck passed;
- Security Evidence run `29306099088`: CodeQL, Dependency Review, Docker build, SPDX SBOM and zero High/Critical Grype gate passed;
- dated release-candidate baseline and closure consistency test are being added without runtime, console or OAuth changes.

## Remaining release gates

1. Run every required local gate on the closure tree.
2. Commit and publish the closure documentation/tests on `p9-brain`.
3. Observe fresh PR checks for the new SHA; do not reuse prior-SHA evidence.
4. Merge PR #4 only with every required job green.
5. Configure the existing app `jqf7qz5ensoqtvl1tb197gcv` with a persistent `/brain` mount and `MCP_DEVBOX_BRAIN_ROOT=/brain`, preserving `/repos` and `/state`.
6. Deploy once through a reviewed plan, observe the same deployment to terminal state, verify exact commit/health/67 tools/hash/logs/Brain readiness, and run `mcp-catalog-smoke` plus `brain-smoke` without private output.
7. Create annotated tag `p9` only after production verification.
8. Only then receive/audit the Opus/Fable frontend proposal and create an independent BIOS Operations Console branch/spec. Edge, WSL/Parrot workcells and HTB remain separate later products.
