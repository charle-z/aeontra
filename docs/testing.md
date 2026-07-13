# Testing and verification

This document records reproducible local commands, environment prerequisites, and
honest gate status. A gate is never described as green when it did not execute.

## Fast local suite

```text
go fmt ./...
go test ./... -count=1
go vet ./...
go build ./...
```

These commands are the per-step baseline. They do not replace race, fuzz, coverage,
or integration gates.

## Race detector baseline — P5 Step 79

Canonical command:

```text
CGO_ENABLED=1 go test -race ./... -count=1
```

Observed environment on the deployed builder container:

```text
GOOS=linux
GOARCH=amd64
CC=gcc
CGO_ENABLED=0
```

Running `go test -race ./... -count=1` returned:

```text
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
```

Result: **blocked before tests executed**. This must not be reported as green and is
not evidence that the repository is race-free.

The production container deliberately uses `CGO_ENABLED=0` for normal builds. P5 does
not mutate persistent `go env` state or weaken runtime configuration merely to run a
development gate. P6 must execute the canonical command in GitHub Actions or another
approved ephemeral Go 1.26 environment with CGO and a C compiler enabled, retain the
result, and make the gate blocking only after it is stable.

## Deterministic concurrency evidence — P5 Step 80

Bounded goroutine tests cover:

- duplicate access requests reuse one id and emit one notification; an approved grant
  is consumed successfully exactly once;
- an action plan executes successfully exactly once under concurrent consume attempts;
- 128 concurrent audit writes produce 128 complete JSONL objects with path redaction;
- OAuth authorization codes and refresh tokens are consumed exactly once, while
  concurrent access-token put/get operations remain consistent.

These tests passed immediately against the existing mutex boundaries. They provide
correctness evidence under interleaving but do not replace the still-pending
CGO-enabled race detector in P6.

## Fuzz seed coverage — P5 Step 81

Go fuzz targets now cover:

- jail containment: any accepted result remains within the configured root;
- command policy: any allowed result satisfies bare-name, allowlist, injection, blocked
  program, and destructive-invocation gates;
- redaction output idempotence and grant TTL boundaries;
- JSON-RPC single-message and bounded batch response validity;
- action-plan operation binding, expiry, and single-use behavior.

Regular `go test` executes the curated seeds. Longer fuzz runs are deferred to P6 so
they execute in ephemeral CI with bounded time and retained crash corpora. Seed
execution corrected two test assumptions: redaction may re-detect an unchanged
placeholder, and an expired plan is consumed before subsequent replay. No runtime
change was required.

## Package-specific coverage gate — P5 Step 82

Generate and evaluate the profile with:

```text
go test ./... -coverprofile=coverage.out -covermode=atomic -count=1
go run ./cmd/coverage-gate --profile coverage.out
```

Versioned minimums protect security-critical packages rather than relying on one
misleading global percentage:

| Package suffix | Minimum | Step 82 baseline |
|---|---:|---:|
| `internal/policy` | 80% | 84.6% |
| `internal/mcpserver` | 80% | 83.7% |
| `internal/mcpserver/catalog` | 80% | 84.4% |
| `internal/oauth` | 80% | 85.3% |
| `internal/audit` | 80% | 86.2% |
| `internal/tools` | 70% | 73.3% |
| `internal/app` | 65% | 69.0% |
| `internal/grantadmin` | 55% | 59.6% |

The gate fails with an explicit missing package error when a threshold package is
absent, when a profile is malformed, or when a package drops below its minimum.
Output is package-specific and actionable. Thresholds are deliberately below the
current measured value so the gate detects regression without turning coverage into
line-count gaming. P6 will run the same two commands as a blocking CI job and retain
only safe coverage artifacts.

## Hermetic integration matrix — P5 Step 83

The local integration suite validates complete contracts without external services,
real credentials, arbitrary processes, or production traffic:

- **stdio/HTTP catalog parity:** both transports return the same ordered 62-tool
  catalog, and HTTP headers match the deterministic catalog identity;
- **bearer fail-closed:** unauthenticated HTTP receives 401 and the server refuses to
  start when neither bearer nor OAuth authentication exists;
- **OAuth challenge:** the synthetic loopback provider exposes protected-resource
  metadata and returns the correct `resource_metadata` challenge;
- **runtime identity:** `/version`, runtime headers, and the in-process catalog agree;
- **local grant approval:** a sensitive read is denied, approved through the local
  admin handler, returned redacted once, and rejected on replay;
- **planned note workflow:** preview, explicit approval, execution, persisted result,
  audit evidence, and single-use replay denial are exercised end to end.

All HTTP interactions use `httptest` and loopback URLs. State lives only in temporary
directories and in-memory stores. The matrix reads no external credentials and makes
no network call outside the test process.

## P5 deeper-testing sequence

1. Add bounded deterministic concurrency tests around shared state.
2. Add fuzz targets with curated safe seed corpora; regular `go test` runs the seeds.
3. Generate a coverage profile and apply package-specific thresholds.
4. Run hermetic integration contracts for transport/auth/catalog/grants/plans.
5. Re-run the race command in the P6 environment.

## Workflow policy guard — P6 Step 86

`internal/workflowpolicy` parses every file under `.github/workflows` and fails the
ordinary Go suite when a workflow violates repository policy. It rejects:

- `pull_request_target`;
- absent or broad root/job permissions, including contents/id-token write;
- missing or over-90-minute job timeouts;
- repository secret references in pull-request workflows;
- production host/deploy/active DAST commands in pull-request jobs;
- actions using `@main`, `@master`, `@latest`, missing refs, or Go tools using
  `@latest`;
- malformed workflows or missing jobs.

The guard permits the narrow `security-events: write` capability required by CodeQL.
The existing CI job gained a 20-minute timeout after the guard detected the missing
bound.

## Core CI — P6 Step 87

`.github/workflows/ci.yml` now contains four blocking jobs:

- **Verify:** formatting, atomic full-suite coverage, package coverage gate, vet, and build;
- **Race detector:** `CGO_ENABLED=1`, `CC=gcc`, and `go test -race ./... -count=1`;
- **Staticcheck:** `honnef.co/go/tools/cmd/staticcheck@v0.7.0` with a writable
  runner-temporary cache;
- **Govulncheck:** `golang.org/x/vuln/cmd/govulncheck@v1.6.0`.

Every job checks out independently, uses Go 1.26.4, has a bounded timeout, and remains
blocking. Local `govulncheck` completed with no vulnerabilities. Local `staticcheck`
initialization was blocked because the production builder HOME is intentionally not
writable; CI sets `XDG_CACHE_HOME` to `${{ runner.temp }}/staticcheck-cache` instead of
mutating the production container. The actual race and staticcheck conclusions must be
observed from GitHub Actions after publication.

## Security and container evidence — P6 Step 88

`.github/workflows/security.yml` adds three bounded jobs:

- **CodeQL:** Go manual build analysis with `github/codeql-action@v4.37.0`; only
  `contents: read` and `security-events: write` are granted;
- **Dependency review:** `actions/dependency-review-action@v5.0.0` runs only for pull
  requests and blocks moderate-or-higher introduced vulnerabilities without PR comments;
- **Container evidence:** builds `mcp-devbox:ci` locally, generates
  `sbom.spdx.json` with `anchore/sbom-action@v0.24.0`, and scans the local image with
  `anchore/scan-action@v7.4.0`, failing on high-or-critical findings.

No registry login, image push, workflow secret, artifact/release upload, production
endpoint, or active DAST exists. SBOM and Grype JSON are verified as non-empty local
files and disappear with the ephemeral runner. Real action conclusions are observed
after branch publication.

## Scheduled fuzzing — P6 Step 89

`.github/workflows/fuzz.yml` runs weekly at `17 3 * * 1` and supports manual dispatch.
A seven-entry matrix maps every Go fuzz function to its exact package. Each target gets
a 30-second fuzz budget, a 10-minute job timeout, `GOMAXPROCS=2`, read-only repository
access, and no secrets or network credentials. Timed fuzzing is not added to push or
pull-request latency.

All seven targets were also executed locally with a one-second budget. That run found
an incomplete test invariant for an expired plan first consumed with the wrong
operation: mismatch does not consume the plan, so the next correctly bound attempt
returns `expired`. The case is now a curated seed and the runtime behavior was not
changed.

## Safety rules

- Do not run active DAST against production.
- Do not persist global Go environment changes on the production container.
- Do not add credentials, real tokens, private targets, or source snapshots to test
  corpora or CI artifacts.
- Tests that need time, randomness, HTTP peers, runners, or stores use injected clocks,
  deterministic ids, loopback synthetic servers, and temporary directories.
- A skipped or prerequisite-blocked gate remains visible as blocked; it is never
  silently converted to a pass.
