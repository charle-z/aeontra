# P6 CI/DevSecOps

Status: in progress on branch `p6-ci-devsecops` from deployed `main` commit `4a68ca054a5f077d62a0f887234866673feb7353`.

Completed:
- Step 85 `fda8c77`: defined P6, recorded the verified P5 deployment, synchronized documentation, and froze the runtime catalog.

Current Step 86 candidate — workflow policy guard:
- added `go.yaml.in/yaml/v3 v3.0.4` as the only runtime-independent parser dependency;
- added `internal/workflowpolicy` with errors and tests for forbidden `pull_request_target`, broad/write permissions, missing/overlong timeouts, PR secret/production actions, mutable action/tool refs, malformed documents, and missing jobs;
- repository test validates every `.github/workflows/*.yml|yaml` file during ordinary `go test ./...`;
- CodeQL's narrow `security-events: write` remains allowed;
- the guard found the existing CI job lacked a timeout, so it now has a 20-minute bound;
- marked T02 complete and synchronized testing/quality docs, capsule, roadmap, and handoff.

Step 86 verification so far:
- RED compile failed before the policy existed;
- repository RED then failed on the missing CI timeout;
- focused policy and repository workflow tests pass after the bound was added;
- remaining: full tests, coverage gate, vet, build, diff review/check, cleanup, and commit.

Next: Step 87 core CI with blocking verify, CGO race, staticcheck, and govulncheck jobs. No publish/merge/deploy until observed GitHub Actions and P6 closure.
