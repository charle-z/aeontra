# P6 CI/DevSecOps

Status: Step 91 remediation candidate on branch `p6-step91-security-remediation`, based on deployed `main` commit `112ca8ce06ffdeba570e486a548801ee21692a6f`.

## Verified deployed base

- Coolify deployment `bn9ehyy686ag4zm5os5cijxl` finished successfully.
- Production serves exact commit `112ca8ce06ffdeba570e486a548801ee21692a6f`.
- Health is green; tool count is 62; catalog hash remains `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

## Observed Actions evidence

- CI run `29263139285`: Verify and Race passed; Staticcheck job `86861702398` and Govulncheck job `86861702481` failed.
- Security run `29263139756`: CodeQL passed; Dependency Review correctly skipped on push; container job `86861700073` failed the unchanged High threshold.
- Exact findings: reachable `GO-2026-5856`; GNU Wget `CVE-2026-58469`, `CVE-2026-58471`, `CVE-2026-58472`; npm `GHSA-52v5-jr5w-gjxr` and `GHSA-c2c7-rcm5-vvqj`; 25 Staticcheck findings.

## Step 91 implementation

- Go pinned to 1.26.5 in `go.mod`, all Actions workflows, `Dockerfile`, and `Dockerfile.validation-runner`.
- Production image uses `golang:1.26.5-alpine3.24`.
- Standalone GNU Wget removed; healthcheck calls the BusyBox applet.
- Final image installs exact `npm@12.0.1`; inspected bundle contains fixed `sigstore@5.0.0` and `picomatch@4.0.5`.
- Three unused declarations removed and 22 Staticcheck error-string findings fixed.
- Regression tests, versioned security report, connector reliability runbook, specs, roadmap, capsule, README, AGENTS, documentation map, and handoff are synchronized.
- Temporary diagnostic helpers were removed from the working tree.

## Verification before commit

- RED policy test failed before implementation and passes after implementation.
- Formatting, ordinary tests, atomic coverage/package gate, vet, build, actionlint, govulncheck, focused workflow/Grype tests, and documentation tests pass.
- Public MCP has no Docker socket; pull-request Actions are authoritative for Staticcheck with runner cache, Docker build, SBOM, and Grype.

## Next exact action

Run the final full gate set and diff check, commit Step 91, publish the branch, open a pull request, and inspect every job/check. Correct reproducible failures before any main fast-forward or deployment.

No public MCP tool/schema/annotation/environment contract change.
