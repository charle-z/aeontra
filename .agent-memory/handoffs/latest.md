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

## Step 91 candidate

- pin Go 1.26.5 in the module, Actions, production image, and validation-runner build;
- remove standalone GNU Wget and use the BusyBox applet for health checks;
- install exact `npm@12.0.1`, whose inspected bundle contains fixed dependency versions;
- fix all Staticcheck findings without changing public tools or successful responses;
- add a regression policy test;
- version the detailed report under `docs/security-reports/`;
- add `docs/runbooks/client-connector-reliability.md` to distinguish expected restart,
  VPS saturation, tool timeout, Coolify failure, and client/transport presentation
  problems using timestamped evidence.

## Verified locally

Formatting, ordinary tests, atomic coverage/package gate, vet, build, actionlint,
govulncheck, and focused workflow/Grype tests pass. Public MCP has no Docker socket,
so Staticcheck with its runner cache, Docker build, SBOM, and Grype must be proven by
pull-request Actions before main changes.

## Next safe step

Finish the diff/docs audit, remove `.tmp` helpers, commit Step 91, publish the branch,
open a pull request, and inspect every job/check. Correct any reproducible failure.
Only after all required checks pass and the final image has zero High/Critical findings
may main be fast-forwarded, deployed, smoke-tested, observed again, and P6 closed.
