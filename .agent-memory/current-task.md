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

## PR #1 evidence

- Current remediation commit: `adc9ad59eab329fa4b654f66a410cecf1fc87791`.
- CI run `29270949295`: success. Verify, Race, Staticcheck, and Govulncheck all passed.
- Security Evidence run `29270949313`:
  - CodeQL: success.
  - Docker build: success.
  - SPDX SBOM generation/verification: success.
  - Grype scan/report verification: success.
  - unchanged High/Critical gate: success, proving zero remaining High/Critical image findings.
  - Dependency Review: failure before analysis because GitHub Dependency Graph is disabled.
- Exact Dependency Review error: `Dependency review is not supported on this repository. Please ensure that Dependency graph is enabled`.

## External blocker

A repository administrator must enable GitHub Dependency Graph for `charle-z/mcp-devbox`, then re-run failed job `86888187941` or failed Security Evidence run `29270949313`. The connected GitHub tools expose admin repository metadata but do not expose the repository security-setting mutation. Do not bypass the check with skip logic, `continue-on-error`, severity reduction, or allowlisting.

## Next exact actions after Dependency Graph is enabled

1. Re-run the failed Dependency Review job and require success.
2. Record the latest run evidence in the versioned report.
3. Fast-forward `main`, publish it, deploy only application `jqf7qz5ensoqtvl1tb197gcv`, preserve the deployment ID, and smoke-test exact commit/health/62 tools/hash.
4. Observe post-merge Actions, create the P6 baseline/audit, close P6, then create a fresh P7 structured-observability branch/spec.

No public MCP tool/schema/annotation/environment contract change.
