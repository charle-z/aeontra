# Spec — P6 CI/DevSecOps

Status: **active** on branch `p6-step91-security-remediation`.
Governed by `.specify/memory/constitution.md`, `docs/quality-gates.md`, and
`docs/testing.md`.

## Goal

Turn the P5 evidence into reproducible, least-privilege GitHub Actions gates without
adding runtime authority, exposing credentials to untrusted pull requests, or scanning
production.

## Required outcomes

1. **Fast pull-request verification:** formatting, atomic full-suite coverage,
   package-specific coverage thresholds, vet, build, integration contracts, and
   generated documentation/catalog assertions.
2. **race detector:** `CGO_ENABLED=1 go test -race ./... -count=1` runs in an ephemeral
   Linux job with a C compiler and is blocking for pull requests and `main`.
3. **Static/vulnerability analysis:** pinned `staticcheck` and `govulncheck` versions
   run with actionable logs and no repository writes.
4. **GitHub security analysis:** CodeQL uses minimal required permissions;
   dependency review runs only where its event context is valid.
5. **Scheduled fuzzing:** every P5 fuzz target runs with an explicit short budget in a
   scheduled/manual workflow; no network credentials or production endpoints exist.
6. **Container evidence:** the Dockerfile builds in CI and produces a local SBOM and
   vulnerability report without pushing an image or exposing registry credentials;
   the release image contains no unresolved High/Critical finding.
7. **Workflow policy tests:** reject `pull_request_target`, broad write permissions,
   mutable unbounded commands, active production DAST, and secret use in fork-facing
   verification jobs.
8. **No public MCP contract change:** P6 changes delivery evidence only; the 62-tool
   catalog and runtime authority remain unchanged.

## Non-goals

- No automatic production deployment from pull requests.
- No active DAST against production.
- No third-party cloud scanner requiring repository secrets.
- No auto-fix commits, dependency-update bot, signing infrastructure, or release
  publishing in this phase.
- No console, profile, asset-broker, orchestrator, or edge implementation.

## Acceptance criteria

- Workflow YAML is parseable and protected by repository tests.
- Local-equivalent commands pass where the builder supports them; environment-specific
  gates are explicitly validated by GitHub Actions after branch publication.
- Race, coverage, integration, static analysis, and vulnerability jobs fail closed.
- Scheduled fuzz targets have fixed names and time budgets.
- Workflow permissions are minimal and secrets are absent from PR verification.
- Exact findings, package/layer provenance, reachability, remediation, and before/after
  workflow evidence are versioned under `docs/security-reports/`.
- P6 closes with a baseline, branch audit, and observed GitHub Actions result before
  deployment.
