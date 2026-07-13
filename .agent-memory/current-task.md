# P6 CI/DevSecOps

Status: in progress on branch `p6-ci-devsecops` from deployed `main` commit `4a68ca054a5f077d62a0f887234866673feb7353`.

Completed:
- Step 85 `fda8c77`: defined P6, recorded the verified P5 deployment, synchronized documentation, and froze the runtime catalog.
- Step 86 `daeb3b4`: added the tested workflow policy guard and bounded the existing CI job.

Current Step 87 candidate — core CI:
- replaced the single minimal Go job with four independent blocking jobs: verify, CGO race, staticcheck, and govulncheck;
- verify enforces gofmt, atomic full-suite coverage, package-specific coverage thresholds, vet, and build;
- race uses `CGO_ENABLED=1`, `CC=gcc`, and `go test -race ./... -count=1` in an ephemeral GitHub runner;
- staticcheck is pinned to `honnef.co/go/tools/cmd/staticcheck@v0.7.0` and uses `${{ runner.temp }}/staticcheck-cache` because the production builder HOME is intentionally not writable;
- govulncheck is pinned to `golang.org/x/vuln/cmd/govulncheck@v1.6.0`;
- all jobs use Go 1.26.4, independent checkout/setup, bounded timeouts, read-only contents permission, and no `continue-on-error`;
- added a workflow contract test that requires all jobs/commands and verifies blocking posture;
- marked T03 complete and synchronized testing, quality-gate, capsule, roadmap, and handoff documentation.

Step 87 verification:
- RED failed because the four jobs and commands did not exist;
- focused workflow-policy tests pass;
- atomic full-suite coverage and all eight package thresholds pass;
- `go vet ./...`, `go build ./...`, and local `govulncheck@v1.6.0 ./...` pass with no vulnerabilities;
- local staticcheck analysis remains environment-blocked by the non-writable production HOME, but the exact pinned binary/version was verified and CI supplies a writable temp cache;
- remaining: final diff review/check, commit, publish, fast-forward main, deploy, and production verification as explicitly requested per point.

Next after release: Step 88 CodeQL, dependency review, Docker build, SBOM, and local vulnerability scan. No runtime/catalog change.
