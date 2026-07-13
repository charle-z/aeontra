# P6 CI/DevSecOps

Status: Step 91 remediation is under review in PR #1 from branch `p6-step91-security-remediation`. Production remains on verified Step 90 commit `112ca8ce06ffdeba570e486a548801ee21692a6f` and deployment `bn9ehyy686ag4zm5os5cijxl`.

## Production base

- Health green; exact Step 90 commit served.
- 62 tools; catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
- No Step 91 deployment has been triggered.

## Original findings

- Reachable `GO-2026-5856`.
- GNU Wget `CVE-2026-58469`, `CVE-2026-58471`, `CVE-2026-58472`.
- npm `GHSA-52v5-jr5w-gjxr` and `GHSA-c2c7-rcm5-vvqj`.
- 25 Staticcheck findings.

## Step 91 commits

- `c54090f2ab01099f3b85e88c45c709bd18876e7d`: Go 1.26.5, fixed bases, Wget removal, npm 12.0.1, Staticcheck fixes, tests, report, and connector runbook.
- Follow-up local correction pending commit: after PR Security run `29270350078`, Grype proved Alpine's bootstrap npm remained under `/usr/lib` despite the safe npm copy under `/usr/local`. `Dockerfile` now runs `apk del npm` after installing npm 12.0.1 and cleaning its cache; policy and documentation tests require this removal.

## PR #1 evidence

- CI run `29270350188`: completed success. Verify, Race, Staticcheck, and Govulncheck all passed.
- Security run `29270350078`: CodeQL passed. Container build, SBOM, and scan steps passed, but the gate still found the duplicate distro npm tree; the follow-up removes it.
- Dependency Review failed before analysis because GitHub Dependency Graph is disabled for `charle-z/mcp-devbox`. Exact error: `Dependency review is not supported on this repository. Please ensure that Dependency graph is enabled`.

## Local verification after follow-up

- `go fmt ./...`.
- `go test ./... -count=1`.
- Atomic coverage plus package coverage gate.
- `go vet ./...`.
- `go build ./...`.
- actionlint v1.7.12.
- govulncheck v1.6.0: no vulnerabilities.
- focused workflowpolicy/grypegate tests.
- `git diff --check`.

## Next exact actions

1. Commit and publish the `apk del npm` follow-up to PR #1.
2. Observe the new CI and Security Evidence runs; require zero High/Critical container findings.
3. A repository administrator must enable GitHub Dependency Graph, then re-run the failed Dependency Review job. The available connectors do not expose that repository security setting, so this is a real human interaction blocker rather than a workflow suppression opportunity.
4. Do not fast-forward `main` or deploy until all required jobs pass.

No public MCP tool/schema/annotation/environment contract change.
