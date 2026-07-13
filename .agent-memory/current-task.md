# P6 CI/DevSecOps

Status: Step 89 candidate on branch `p6-step89-scheduled-fuzz`, based on deployed `main` commit `72cd64d94ae84ac7e644d3f7f1300fca2f44c0e8`.

Deployed completed steps:
- Step 85 `fda8c77`: P6 foundation.
- Step 86 `daeb3b4`: workflow policy guard.
- Step 87 `099ca51`: blocking core CI; published, fast-forwarded, deployed, production-verified.
- Step 88 `72cd64d`: CodeQL, dependency review, local container SBOM/vulnerability evidence; published, fast-forwarded, deployed, production-verified.

Current Step 89 candidate — scheduled fuzz:
- added `.github/workflows/fuzz.yml`, scheduled weekly at `17 3 * * 1` and manually dispatchable;
- seven-entry matrix covers every Go fuzz function discovered in policy, mcpserver, and tools;
- each target gets a 30-second fuzz budget, 10-minute job timeout, `GOMAXPROCS=2`, read-only contents permission, no secrets, and blocking failures;
- workflow target set is tested against the actual `func Fuzz*` declarations so new targets cannot silently miss CI;
- all seven targets passed local one-second timed runs;
- the action-plan fuzz run found an incomplete test invariant for expired plan plus first operation mismatch; mismatch does not consume the plan, so the next correctly bound attempt returns `expired`; this is now an explicit seed and the runtime was not changed;
- no generated crash corpus remains in the repository;
- marked T05 complete and synchronized testing, quality-gate, capsule, roadmap, documentation assertions, and handoff state.

Step 89 verification:
- RED failed because no scheduled fuzz workflow existed;
- workflow policy and exact target-set tests pass;
- all seven local timed fuzz runs pass after correcting the test invariant;
- atomic full-suite coverage and all eight package thresholds pass;
- `go fmt ./...`, `go vet ./...`, and `go build ./...` pass.

Remaining: clean temporary helpers, final diff/check, commit, publish branch, fast-forward `main`, deploy, verify production, then inspect the real GitHub Actions conclusions for Steps 87-89 before P6 closure.
