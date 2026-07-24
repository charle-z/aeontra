# Current task

Historical deployed baseline remains unchanged: this P16 branch has not changed `main`, Coolify, production or real Parrot.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Rootless correction head `732dbb1d961a746a039953fe915f9cbd77215535` passed 15/15 exact-head checks.

## P16 Step 6 candidate

Step 6 admission and fairness is implemented privately under `internal/workqueue` without adding public MCP tools or execution authority.

Implemented and tested:

- closed integer resource vectors for CPU millis, memory MiB, I/O weight, PIDs and slots;
- rejection of negative, fractional, NaN, infinity, overflow and unknown dimensions;
- administrator-owned pool/profile registry with fixed pool budgets and per-job maxima;
- hard conjunctive admission that cannot exceed any dimension;
- deterministic Deficit Round Robin per workspace with bounded deficits;
- bounded aging that selects oldest eligible work before starvation;
- duplicate job identity rejection and cleanup of deficits for inactive workspaces;
- deterministic tie-breaking by workspace, creation time and job ID;
- EWMA history isolated by pool/device/profile, minimum sample threshold and 4x outlier clamp;
- shadow score that is observational only and cannot mutate target, authorization or resources;
- explicit estimate disable, re-enable, per-key reset and global reset;
- bounded aggregate queue metrics and informational wait estimates without IDs, paths or payloads;
- terminal TTL cleanup that never removes queued/blocked/leased jobs or referenced dependencies.

Documentation:

- `docs/workqueue-admission-fairness.md`;
- Step 6 checklist in `specs/007-global-work-scheduler/tasks.md` is complete.

Validation on the current tree:

- `go test ./... -count=1` green;
- `internal/workqueue` coverage 77.4%, above the blocking 70% threshold;
- `go vet ./...` green;
- Staticcheck v0.7.0 green;
- `go build ./...` green;
- `git diff --check` green.

Next: repeat final regression after the fairness hardening, commit `Step 6: Add admission and fair scheduling`, publish to PR #48, hold the exact SHA until all checks are terminal, then begin Step 7 rootless builder spike only after green CI.
