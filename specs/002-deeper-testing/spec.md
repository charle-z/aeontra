# Spec — P5 deeper testing

Status: **active** on branch `p5-deeper-testing`.
Governed by `.specify/memory/constitution.md` and `docs/quality-gates.md`.

## Goal

Increase confidence in the deployed security/control-plane behavior through deeper,
repeatable tests without adding runtime authority, slowing normal MCP calls, or
changing the public catalog. P5 is test engineering; P6 will wire proven gates into
CI/CD.

## Required outcomes

1. **race detector:** the complete Go suite runs under `go test -race` where the
   platform supports CGO, with deterministic concurrency tests for policy grants,
   plans, audit, OAuth stores, and HTTP state.
2. **fuzz and adversarial seeds:** stable fuzz targets cover path containment,
   command policy, secret redaction, JSON-RPC parsing/batches, and plan/grant inputs.
   Normal `go test` executes the seed corpus; scheduled fuzz time belongs to P6/P7
   automation.
3. **coverage:** generate a Go coverage profile and enforce explicit minimums for
   security-critical packages. Thresholds must be realistic, versioned, and fail with
   actionable package-level output rather than a misleading global percentage.
4. **integration contracts:** exercise stdio/HTTP parity, bearer/OAuth fail-closed
   behavior, deterministic catalog identity, grant lifecycle, action-plan replay,
   and deployment-safe runtime diagnostics.
5. **no public MCP contract change:** P5 must preserve all 62 tool names, order,
   schemas, aliases, annotations, handlers, approval posture, environment names, and
   deterministic catalog hash.

## Non-goals

- No new MCP tools or execution authority.
- No console, asset broker, universal profile, edge-agent, or orchestrator work.
- No active DAST against production.
- No permanent dependency/tool installation merely to make one local gate pass.
- No flaky timing assertions or tests that require external credentials/services.

## Acceptance criteria

- Race suite has a documented command, result, and platform limitation behavior.
- Every fuzz target has meaningful seed cases and no secret-bearing corpus files.
- Coverage gate has unit tests, documented thresholds, and a reproducible command.
- Integration tests are hermetic or use synthetic local servers only.
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, documentation tests,
  branch audit, and production catalog smoke pass before closure.
- P5 is published/deployed only after the full phase baseline and explicit release
  verification; tests alone never justify claiming a runtime security guarantee.
