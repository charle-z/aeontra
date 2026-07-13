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

## Safety rules

- Do not run active DAST against production.
- Do not persist global Go environment changes on the production container.
- Do not add credentials, real tokens, private targets, or source snapshots to test
  corpora or CI artifacts.
- Tests that need time, randomness, HTTP peers, runners, or stores use injected clocks,
  deterministic ids, loopback synthetic servers, and temporary directories.
- A skipped or prerequisite-blocked gate remains visible as blocked; it is never
  silently converted to a pass.
