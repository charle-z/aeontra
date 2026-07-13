# P6 CI/DevSecOps

Status: Step 91 technical remediation is green in PR #1 on branch `p6-step91-security-remediation`. Production remains on verified Step 90 commit `112ca8ce06ffdeba570e486a548801ee21692a6f`; no Step 91 deployment has been triggered.

## Production base

- Coolify deployment `bn9ehyy686ag4zm5os5cijxl`: finished.
- Exact Step 90 commit served; health green.
- 62 tools; catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

## Remediated findings

- Reachable `GO-2026-5856` fixed by Go 1.26.5.
- GNU Wget `CVE-2026-58469`, `CVE-2026-58471`, `CVE-2026-58472` removed with the standalone package.
- npm `GHSA-52v5-jr5w-gjxr` and `GHSA-c2c7-rcm5-vvqj` fixed by installing `npm@12.0.1` and deleting Alpine's vulnerable bootstrap npm tree with `apk del npm`.
- All 25 Staticcheck findings fixed.

## PR #1 evidence before Dependency Graph activation

- Current remediation commit: `6b692892427a05f4cdfad48d476781bd79111cf9`.
- CI run `29271700972`: success. Verify, Race, Staticcheck, and Govulncheck all passed.
- Security Evidence run `29271701096`:
  - CodeQL: success.
  - Docker build: success.
  - SPDX SBOM generation/verification: success.
  - Grype scan/report verification: success.
  - unchanged High/Critical gate: success, proving zero remaining High/Critical image findings.
  - Dependency Review failed before analysis because GitHub Dependency Graph was disabled.

## Dependency Graph activation

- The repository administrator confirmed that GitHub Dependency Graph is now enabled.
- The production container's `GITHUB_TOKEN` exists but lacks `Actions: write`; a direct rerun API request returned 403.
- The MCP command policy also rejects the `gh` binary, so no unsafe bypass or policy weakening was used.
- A minimal versioned state update will be committed to the existing PR branch. Its push will naturally create fresh PR workflows and execute Dependency Review with Dependency Graph enabled.

## Next exact actions

1. Commit and publish this state update to trigger fresh PR workflows.
2. Require CI, CodeQL, container/SBOM/Grype, and Dependency Review all to pass.
3. Fast-forward `main`, publish it, deploy only application `jqf7qz5ensoqtvl1tb197gcv`, preserve the deployment ID, and smoke-test exact commit/health/62 tools/hash.
4. Observe post-merge Actions, create the P6 baseline/audit, close P6, then create a fresh P7 structured-observability branch/spec.

No public MCP tool/schema/annotation/environment contract change.
