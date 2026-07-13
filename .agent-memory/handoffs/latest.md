# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p6-step91-security-remediation`
Deployed base: `main` at `112ca8ce06ffdeba570e486a548801ee21692a6f`

## Current phase

Step 90 is published, fast-forwarded, deployed, and production-verified at 62 tools
with catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
P6 CI/DevSecOps Step 91 is active and governed by `specs/003-ci-devsecops/`.
The workflow policy guard and blocking CGO race remain part of the required gates.

## Exact observed failures

- CI run `29263139285`: Verify and CGO race passed; Staticcheck and Govulncheck failed.
- Security run `29263139756`: CodeQL passed; Dependency Review correctly skipped on
  push; the container gate reported five High findings.
- Reachable Go finding: `GO-2026-5856`.
- Final-image findings: three GNU Wget CVEs plus npm `sigstore` and `picomatch` GHSAs.
- Staticcheck: three unused declarations and 22 capitalized error strings.

## Step 91 remediation

- Go 1.26.5 is pinned in the module, Actions, production image, and validation runner.
- Standalone GNU Wget is removed; health checks use the BusyBox applet.
- Exact `npm@12.0.1` is installed and Alpine's vulnerable bootstrap npm is removed.
- All Staticcheck findings are fixed without public contract changes.
- Regression tests, the detailed security report, and connector reliability runbook
  are versioned.

## Verified

- Current remediation commit: `adc9ad59eab329fa4b654f66a410cecf1fc87791`.
- CI run `29270949295` passed Verify, Race, Staticcheck, and Govulncheck.
- Security run `29270949313` passed CodeQL, image build, SPDX SBOM generation and
  verification, Grype scan/report verification, and the unchanged High/Critical gate.
- The final image has zero remaining High/Critical findings.
- Dependency Review cannot execute because GitHub Dependency Graph is disabled for
  the repository. Exact failed job: `86888187941` in run `29270949313`.
- The connector exposes repository admin metadata but not the security-setting write.
  A repository administrator must enable Dependency Graph and re-run the failed job;
  do not skip, soften, or mark the check non-blocking.

## Next safe step

After a repository administrator enables GitHub Dependency Graph, re-run Dependency
Review and require it to pass. Then record the final run, fast-forward/publish `main`,
deploy only application `jqf7qz5ensoqtvl1tb197gcv`, smoke-test exact commit/health/62
tools/hash, observe post-merge Actions, create the P6 baseline/audit, and close P6.
