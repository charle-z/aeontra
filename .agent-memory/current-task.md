# P6 CI/DevSecOps

Status: Step 88 candidate on branch `p6-step88-security-evidence`, based on deployed `main` commit `099ca51de0db536b31dfe5c18a81f4a7bcf7ca97`.

Deployed completed steps:
- Step 85 `fda8c77`: P6 foundation.
- Step 86 `daeb3b4`: workflow policy guard.
- Step 87 `099ca51`: blocking verify, CGO race, staticcheck, and govulncheck CI; published, fast-forwarded, deployed, and production-verified with 62 tools and unchanged catalog hash.

Current Step 88 candidate — security/container evidence:
- added `.github/workflows/security.yml` with three bounded, blocking jobs;
- CodeQL uses `github/codeql-action/init/analyze@v4.37.0`, Go 1.26.4, manual build, `contents: read`, and only `security-events: write` beyond root read access;
- dependency review uses `actions/dependency-review-action@v5.0.0`, executes only on pull requests, blocks moderate-or-higher introduced vulnerabilities, and never writes PR comments;
- container evidence builds `mcp-devbox:ci` locally, creates an SPDX JSON SBOM with `anchore/sbom-action@v0.24.0`, and scans the local image with `anchore/scan-action@v7.4.0`, failing on high-or-critical findings;
- no secrets, registry login, image push, artifact/release upload, production endpoint, or active DAST exists;
- added exact workflow contract tests and the existing workflow policy validates the new file;
- marked T04 complete and synchronized testing, quality-gate, capsule, roadmap, documentation assertions, and handoff state.

Step 88 verification:
- RED failed because `security.yml` did not exist;
- focused workflow policy/contract tests pass;
- atomic full-suite coverage and all eight package thresholds pass;
- `go fmt ./...`, `go vet ./...`, and `go build ./...` pass;
- real CodeQL/dependency/container conclusions remain to be observed after publication.

Remaining: clean temporary helpers, final diff/check, commit, publish branch, fast-forward `main`, deploy, verify production, then open Step 89 scheduled fuzz from the deployed commit.
