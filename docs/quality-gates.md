# Quality and security gates

This document defines the intended delivery gates. A gate is added only when its
failure is actionable and its runtime cost matches where it runs. Current commands,
prerequisites, and honest execution status are recorded in `docs/testing.md`.

## Pull request: fast feedback

Coverage prerequisite added by P5:

```text
go test ./... -coverprofile=coverage.out -covermode=atomic -count=1
go run ./cmd/coverage-gate --profile coverage.out
```

The gate is package-specific, rejects a missing package, and never substitutes one
global coverage percentage for critical-package evidence. P6 will make it blocking.

Workflow policy (always through `go test ./...`): dangerous triggers, permissions,
secrets, mutable versions, missing timeouts, and production actions fail before merge.

Core CI is split into independent blocking verify, CGO race, staticcheck, and
govulncheck jobs so one failure remains attributable.

Core Go:

- formatting check;
- `go test ./... -count=1`;
- `go vet ./...`;
- `go build ./...`;
- `staticcheck`;
- focused Semgrep rules;
- generated catalog/docs consistency.

Console:

- frozen pnpm install;
- Astro/TypeScript check;
- unit and component tests;
- production build;
- focused Semgrep rules.

## Main branch or protected merge

Security evidence added by P6:

- CodeQL Go analysis with minimal `security-events: write`;
- pull-request dependency review at moderate severity;
- local Docker build, SPDX JSON SBOM, and blocking high-severity Grype scan;
- no registry credentials, push, production DAST, or secret-bearing artifacts.

- race detector with CGO enabled;
- coverage report and security-package thresholds;
- `govulncheck`;
- CodeQL for Go and JavaScript/TypeScript;
- dependency review;
- container build and vulnerability scan;
- SBOM generation;
- integration tests for HTTP/OAuth/catalog/deployment contracts.

## Scheduled or ephemeral staging

P6 scheduled fuzz runs every known target in a bounded matrix; adding a Go fuzz
function without adding its workflow entry fails repository tests.

- fuzzing and longer adversarial tests;
- recovery/rollback drills;
- Playwright smoke tests;
- ZAP baseline/passive DAST;
- authenticated passive DAST with synthetic users when the private console exists;
- TTL cleanup verification for staging and retained artifacts.

## Container gates

Every image records:

- runtime user;
- base image and digest/pin policy;
- exposed ports;
- mounts and capabilities;
- writable paths;
- healthcheck;
- final image contents and size;
- SBOM and vulnerability results;
- justified exceptions.

Multi-stage builds must copy only the binary/runtime assets required by the final
image. Build SDKs, package caches, source trees, and unrelated libraries do not enter
the runtime layer unless an explicit builder capability requires them.

## DAST lifecycle

1. Create an isolated staging application with synthetic configuration.
2. Assign an expiry timestamp/TTL.
3. Wait for readiness and verify the expected commit.
4. Run smoke tests and passive/baseline DAST.
5. Store bounded reports as CI artifacts.
6. Destroy the application immediately after success.
7. A scheduled cleanup removes leaked/failed staging resources after the TTL.

Production is never the target of active scanning from this pipeline.

## Failure policy

A required gate blocks merge or deployment. Experimental gates begin in report-only
mode with a dated plan to become blocking. Suppressions require a narrow rule,
justification, owner, and expiry date; global or permanent ignores are not accepted.
