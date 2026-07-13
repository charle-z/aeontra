# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p6-step90-observed-actions`
Deployed base: `main` at `e70b10351e6820a4e9f6c827dcb11acc57dbb9c1`

## Current phase

P5 is published, fast-forwarded, deployed, and production-verified at 62 tools with
catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
P6 CI/DevSecOps is active and defined by `specs/003-ci-devsecops/`.

## P6 scope

- tested workflow policy guard;
- blocking format/test/coverage/vet/build/static/vulnerability and CGO race jobs;
- CodeQL, dependency review, Docker build, SBOM and local vulnerability evidence;
- bounded scheduled fuzz for every P5 fuzz target;
- no PR deployment, production DAST, or runtime/catalog change.

## Next safe step

T01-T05 are deployed. Initial real Actions runs showed invalid job-level
`runner.temp` in CI and a real High/Critical image finding. Step 90 fixes the workflow
schema and adds actionable Grype annotations. Publish/deploy it, observe the exact CVE,
remediate the image, then mark T06 and close P6.
