# Current task — P15 post-merge documentation closure

PR #39 passed all 15 checks, merged into `main`, and production runtime verification returned commit `f9577f791e1a566845fbbac59571cd72aa85a47f`, 98 tools and catalog hash `sha256:8a9a637f2817e9e2824ac9756c5cf8f5146fee3b6ee5515ea2f72903ed922e12`.

This follow-up branch, `docs/p15-postmerge-closure`, removes one self-invalidating documentation pattern introduced by PR #39: mutable current-state documents and tests must not hardcode the exact `main` SHA as permanent truth. Release `p15.0.5` remains anchored at `5048a5aa0e0d57d67df3680112aee0d47c954543`; production tracks `main`, and `system_runtime_info` is the authority for the exact live commit.

Historical deployed anchors remain explicit because later phases are additive:

- P8.1 Console 2.0 is closed and deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, tagged `p8.1`, with its historical 67-tool milestone and `not_paired` Edge state preserved as closure evidence.
- P9 Brain is deployed as the successor to P8.1; later Edge, workcell and HTB phases do not rewrite that evidence.
- P13 opaque continuation, P14 authorized HTB actions and P15 signed Edge are later deployed successors.

The current evidence boundary remains unchanged: the latest repository-recorded real-host Parrot installation proof is `p15.0.4`. A `p15.0.5` device update, local Git credential entry and real private-repository smoke are not claimed.

Changes in this closure:

1. `AGENTS.md` and `docs/context-capsule.md` distinguish the stable release anchor from the moving deployed branch.
2. The capsule records PR #39 deployment evidence without pretending that `f9577f7` will remain the latest commit forever.
3. The development Edge Git paragraph now states that PR #38 and the signed release are complete while Parrot validation remains separate.
4. `docs/p15_documentation_truth_test.go` treats `5048a5a` as the release commit and requires `system_runtime_info` as the live-commit authority.

Run documentation tests, `go vet`, `go build`, and `git diff --check`; then publish and merge only with all exact-head checks green.
