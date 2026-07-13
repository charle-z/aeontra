# P2 capability service split

Status: complete and merge-ready on branch `p2-capability-services`; explicit owner approval is still required before publication, merge, or deployment.

Completed commits:
- Step 54 `2ef5414`: introduced central `serviceCore` and five capability services behind the compatible `Service` facade.
- Step 55 `b9556a3`: moved jailed workdir resolution to the shared core.
- Step 56 `0992d46`: moved repository/filesystem/memory/notes behavior to `RepositoryCapability`.
- Step 57 `d76a9a0`: moved Git behavior to `GitCapability`.
- Step 58 `570f042`: moved GitHub/source-hosting behavior to `SourceCapability`.
- Step 59 `6056b5a`: moved Coolify/platform behavior to `PlatformCapability`.
- Step 60 `bede1c2`: moved command/test/sandbox/validation/privileged behavior to `ExecutionCapability`.
- Step 61 `841db02`: made `Service` a delegating configuration facade and added an AST boundary guard.

Current Step 62 closure candidate:
- refreshed `origin/main` and confirmed deployed base `0de426e088466a1421b527f8ce1bf83cb53bd2a9`;
- added `docs/p2_closure_test.go` and `docs/baselines/2026-07-13-p2.md`;
- synchronized README, AGENTS, and the context capsule with deployed P1 and merge-ready P2 state;
- updated the P1 closure test so it truthfully requires deployed P1 rather than the obsolete pre-merge branch state;
- reviewed Step 54-61 commit bodies: numbered commits, no AI signatures or `Co-Authored-By` lines;
- reviewed changed files: source, tests, docs, and agent memory only; no binary, SDK, cache, secret file, token, or credential-bearing configuration.

Final P2 verification:
- capability compile-time interface assertions passed;
- Service facade AST boundary test passed;
- P1 and P2 closure documentation tests passed;
- `go fmt ./...` produced no changes;
- `go test ./... -count=1` passed;
- `go vet ./...` passed;
- `go build ./...` passed;
- `git diff --check` passed;
- production catalog smoke passed at commit `0de426e088466a1421b527f8ce1bf83cb53bd2a9`, 62 tools, and hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Verdict: P2 is ready to publish and fast-forward into `main`, followed by deployment and runtime/client verification. No P2 publish, merge, or deploy has occurred yet.
