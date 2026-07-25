# Current task

Historical deployed successor truth remains explicit: P8.1 is deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its
successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Current production and canonical `main` are synchronized at
`1e418658102645ed45b739a9a84562704cae65ab`. The live runtime reports version
`0.2.0`, protocol `2024-11-05`, 102 tools and catalog hash
`sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b`.

Branch: `fix/p16-calibration-review`.

## Trigger

PR #49 closed the clean-host prerequisite defects and merged with 16/16 checks green.
Production is healthy on that merge. The public MCP still cannot execute the host-root
bootstrap because privileged profiles are administrator-disabled and, more
fundamentally, the Coolify container has no host systemd or filesystem authority.
Step 8 therefore remains correctly blocked on real VPS calibration.

The remaining Step 7 weakness was manual interpretation of the 50/65/80 evidence.
A malformed or incomplete matrix could otherwise lead to an inconsistent quota choice.

## Candidate

- adds executable `packaging/builder/review-vps-calibration.sh`;
- validates exactly six measurements for 50/65/80 no-cache and cached builds;
- rejects malformed, duplicate, missing, oversized, failed, OOM, unhealthy, 502,
  missing-artifact and absent-cache-reuse evidence;
- writes root-private `selection.tsv`, `selected-quota-percent` and
  `selection-policy`;
- selects the lowest hard-eligible quota whose no-cache duration is at most 135 percent
  and cached duration at most 125 percent of the fastest eligible run;
- records `none` and fails closed when no quota satisfies the policy;
- integrates the selector into the fixed calibrator and exact-commit bootstrap;
- documents the deterministic policy and updates only Step 7 tasks already proven by
  repository/CI evidence.

No public tool, root shell, Docker socket, production worker, scheduler bypass or Step 8
execution path is added.

## Validation

Green on the exact staged tree:

- `go test ./packaging/builder -count=1`;
- `go test ./docs -count=1`;
- `go test ./internal/buildspike ./internal/coveragegate -count=1`;
- full configured repository test suite;
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`.

Staticcheck is not allowlisted in this local workcell and remains mandatory in exact-head
CI. Next: commit, publish, open a minimal PR, require every exact-head gate green, merge
and deploy. Then execute the one reviewed root bootstrap on the VPS for the exact green
merge commit, review its private selection evidence, freeze the selected quota in a
separate tested commit, and only then begin Step 8.
