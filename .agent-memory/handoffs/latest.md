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

## PR #1 observation

- Commit `c54090f2ab01099f3b85e88c45c709bd18876e7d` is published in PR #1.
- CI run `29270350188` passed Verify, Race, Staticcheck, and Govulncheck.
- Security run `29270350078` passed CodeQL, image build, SBOM generation, and scan
  execution, but Grype proved the vulnerable Alpine npm tree remained under `/usr/lib`
  beside the fixed global npm under `/usr/local`.
- The follow-up removes the bootstrap package with `apk del npm` after installing
  `npm@12.0.1`; local full gates pass again and a new PR run is required.
- Dependency Review cannot execute because GitHub Dependency Graph is disabled for
  the repository. The connector cannot change that repository security setting. A
  repository administrator must enable Dependency Graph and re-run the failed job;
  do not skip, soften, or mark the check non-blocking.

## Next safe step

Commit and publish the `apk del npm` follow-up, then inspect the replacement PR runs.
Require zero High/Critical findings. After a repository administrator enables GitHub
Dependency Graph, re-run Dependency Review and require it to pass. Only then may main
be fast-forwarded, deployed, smoke-tested, observed again, and P6 closed.
