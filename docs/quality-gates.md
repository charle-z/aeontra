# Quality and security gates

This document defines the intended delivery gates. A gate is added only when its
failure is actionable and its runtime cost matches where it runs. Current commands,
prerequisites, and honest execution status are recorded in `docs/testing.md`.

## Pull request: fast feedback

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

- race detector with CGO enabled;
- coverage report and security-package thresholds;
- `govulncheck`;
- CodeQL for Go and JavaScript/TypeScript;
- dependency review;
- container build and vulnerability scan;
- SBOM generation;
- integration tests for HTTP/OAuth/catalog/deployment contracts.

## Scheduled or ephemeral staging

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
