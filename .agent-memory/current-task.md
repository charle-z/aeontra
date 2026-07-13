# P5 deeper testing

Status: in progress on branch `p5-deeper-testing` from deployed `main` commit `4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`.

Completed:
- Step 78 `47033fa`: defined P5 and recorded the verified P4 deployment.
- Step 79 `58146f9`: documented the CGO-blocked race baseline and assigned the real gate to P6.
- Step 80 `9bf0f1a`: added deterministic concurrency evidence for grants, plans, audit, and OAuth.
- Step 81 `adf4e02`: added curated safe fuzz targets for policy, JSON-RPC, grants, and plans.

Current Step 82 candidate — package-specific coverage gate:
- measured the full suite with atomic coverage and rejected a misleading global-only threshold;
- added `internal/coveragegate` with a tested Go coverprofile parser and actionable missing/malformed/below-minimum failures;
- added `cmd/coverage-gate` with stable exit codes, default security-package thresholds, and optional repeated `--min package=percent` overrides;
- versioned minimums: policy/mcpserver/catalog/oauth/audit 80%, tools 70%, app 65%, grantadmin 55%;
- regenerated the final profile and all eight packages pass at 84.6%, 83.7%, 84.4%, 85.3%, 86.2%, 73.3%, 69.0%, and 59.6% respectively;
- marked T06 complete and synchronized testing docs, quality gates, README, AGENTS, capsule, roadmap, and handoff.

Step 82 verification:
- RED failed because no parser, thresholds, or CLI existed;
- parser/CLI/documentation tests passed for valid profiles, missing packages, malformed profiles, low thresholds, command usage, and actionable output;
- `go test ./... -coverprofile=/tmp/mcp-devbox-cover.out -covermode=atomic -count=1` passed;
- `go run ./cmd/coverage-gate --profile /tmp/mcp-devbox-cover.out` passed all eight thresholds;
- ordinary full tests, `go fmt ./...`, `go vet ./...`, and `go build ./...` passed;
- remaining: remove temporary helper, final diff/check, stage, and commit.

Next: Step 83 hermetic integration contract matrix, then P5 closure. Race and timed fuzz execution remain P6 work. No public MCP contract change.
