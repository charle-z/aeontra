# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p6-ci-devsecops`
Deployed base: `main` at `4a68ca054a5f077d62a0f887234866673feb7353`

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

T01 foundation and T02 workflow policy are complete. Next replace the minimal CI
with blocking verify/race/static/vulnerability jobs, then add security and fuzz workflows.
