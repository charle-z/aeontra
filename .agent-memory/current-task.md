# P6 CI/DevSecOps

Status: Step 90 candidate on branch `p6-step90-observed-actions`, based on deployed `main` commit `e70b10351e6820a4e9f6c827dcb11acc57dbb9c1`.

Deployed completed steps:
- Step 85 `fda8c77`: P6 foundation.
- Step 86 `daeb3b4`: workflow policy guard.
- Step 87 `099ca51`: blocking core CI.
- Step 88 `72cd64d`: CodeQL, dependency review, container SBOM/vulnerability evidence.
- Step 89 `e70b103`: bounded scheduled fuzz.

Observed GitHub Actions evidence:
- CI run `29260843017` failed before creating jobs. Pinned actionlint reproduced the exact schema error: `${{ runner.temp }}` was used in job-level env where the runner context is unavailable.
- Security run `29260848623`: CodeQL passed; dependency review skipped correctly for a push; container scan failed because at least one High/Critical vulnerability exists.
- The public check annotation confirmed `Failed minimum severity level. Found vulnerabilities with level 'high' or higher`; the severity threshold has not been lowered.

Current Step 90 candidate:
- moved `XDG_CACHE_HOME=${{ runner.temp }}/staticcheck-cache` to the Staticcheck step, where the runner context is valid;
- added pinned `actionlint@v1.7.12` to the blocking Verify job and contract tests so workflow schema/expression errors fail before future publication;
- added tested `internal/grypegate` and `cmd/grype-gate` to parse Grype JSON, reject malformed/unknown data, sort High/Critical findings, emit bounded escaped GitHub annotations with CVE/package/version/fix/type/location, and fail closed;
- changed Anchore scan action to generate the JSON report without terminating the step, then apply the same High threshold through `grype-gate`; no finding is suppressed or downgraded;
- updated workflow contracts, testing/quality docs, capsule, roadmap, handoff, and documentation assertions.

Step 90 verification so far:
- RED reproduced missing Grype diagnostics and workflow-schema evidence;
- grype parser/CLI tests pass, including annotation injection/escaping cases;
- workflow policy and exact CI/security contracts pass;
- `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` passes on all workflows;
- remaining: full atomic coverage suite, package gate, vet, build, cleanup, diff review/check, commit, publish/deploy, and observe exact image findings from GitHub.

T06 remains open until the corrected GitHub Actions runs complete and the image finding is remediated. No public MCP contract change.
