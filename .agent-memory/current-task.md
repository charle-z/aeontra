# Current task — P15 documentation truth closed

Branch: `docs/p15-documentation-truth` from `origin/main` at `5048a5aa0e0d57d67df3680112aee0d47c954543` (`p15.0.5`). The documentation-only closure is committed locally; use the branch `HEAD` as the exact commit authority.

Goal completed: synchronize public and operator-facing documentation with repository and deployment evidence after P13–P15 without rewriting historical baselines.

Verified source state:

- PR #29 (P14) is merged and all 13 checks passed.
- PR #38 (P15 development Edge Git) is merged and all 15 checks passed.
- `main` and `p15.0.5` contain 98 tools with catalog hash `sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12`.
- The configured Coolify application tracks `main` and reports `running:healthy`.
- The last repository-recorded real-host Parrot installation proof is `p15.0.4`; a `p15.0.5` device update and real private-repository smoke are not claimed by this documentation task.

Historical deployed foundation required by documentation tests:

- P8.1 Console 2.0 is closed, deployed and tagged at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed as the successor milestone; its historical 67 tools and `not_paired` evidence remain preserved.
- P13 opaque workspace continuation, P14 first-class authorized HTB actions and the P15 signed Edge line are additive successors.

Documentation result:

1. README distinguishes source release, VPS deployment and real Edge installation evidence; records P13–P15; preserves historical tool milestones; removes the stale console candidate footer.
2. `SECURITY.md` and `docs/security.md` describe profile-specific isolation, signed Edge bundles, target/VPN-bound HTB authority and private Edge Git without claiming universal OS isolation or egress control.
3. `AGENTS.md`, `docs/context-capsule.md`, `docs/documentation-map.md` and the handoff identify `p15.0.5`, 98 tools and the current evidence boundary.
4. `docs/p15_documentation_truth_test.go` prevents stale 85-tool/current-candidate and obsolete security claims from returning.

Verification:

- `go test ./docs -count=1` — pass.
- Aggregate `go test ./... -count=1` — the command runner terminated the long process with `signal: killed` after all displayed packages passed; no test failure was reported.
- The remaining package batch from `internal/modelturn` through `profiles` — pass, completing package coverage when combined with the aggregate output.
- `go vet ./...` — pass.
- `go build ./...` — pass.
- `git diff --check` — pass.

No push, pull request, deployment or Parrot update has been performed. A later explicit task may publish this branch or separately verify/install `p15.0.5` on Parrot.
