# P4 targeted Layer-1 hardening

Status: complete and merge-ready on branch `p4-l1-hardening`; release is explicitly authorized but has not yet occurred.

Completed commits:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative/workspace-controlled PATH targets.
- Step 72 `9af06c4`: enforced grant TTL bounds in the central policy core.
- Step 73 `fe2e903`: expired, capped, pruned and deduplicated pending sensitive-read requests.
- Step 74 `78a6fff`: synchronized and tested project documentation state.
- Step 75 `fb6b796`: redacted secret-bearing audit file paths.
- Step 76 `002bd78`: bounded HTTP JSON-RPC batches and rejected empty batches.

Current Step 77 closure candidate:
- added `docs/baselines/2026-07-13-p4.md` with base, implementation head, security results, audit evidence, verification and release posture;
- added `docs/p4_closure_test.go`;
- transitioned capsule, roadmap, Layer-1 follow-on tasks, README, AGENTS, documentation-state assertions and latest handoff from active P4 to complete/merge-ready P4;
- refreshed `origin/main` and confirmed deployed base `dd055e251c455086ddcb02bc302d9f406b05d6ce`;
- reviewed Step 70-76 commit messages: no AI signatures or `Co-Authored-By` lines;
- reviewed changed files: Go source/tests, docs, specs, constitution and agent memory only; no binaries, SDKs, caches, tokens, secret files or credential-bearing configuration.

Step 77 verification:
- RED failed because the P4 baseline and completed phase state did not exist;
- P4 closure and project documentation tests passed after synchronization;
- `go fmt ./...`, `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and `git diff --check` passed;
- production smoke remains healthy at P3 commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`, 62 tools and catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Verdict: P4 is ready for the authorized sequence: publish feature branch, fast-forward `main`, deploy the existing Coolify application, verify production, then create a fresh P5 branch and record P4 as deployed before deeper testing.
