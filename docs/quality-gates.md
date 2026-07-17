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
global coverage percentage for critical-package evidence. P6 makes it blocking. P7
adds `internal/observability` at a 70% minimum against a measured 74.4% baseline.
P8 adds `internal/console` at an 80% minimum against a measured 84.3% baseline.
P9 Step 7 keeps `internal/brain` at 81.2%, `internal/tools` at 73.9%,
`internal/app` at 71.3%, `internal/mcpserver` at 82.6%, and
`internal/mcpserver/catalog` at 85.6%, all above their minimums. Runtime tests make
configured Brain startup fail closed, packaging reserves the dedicated volume, and the
remote smoke exposes no note content or credential. The prior 62-tool contract
hash/order remain locked inside the 67-tool candidate. Deployment remains pending.

P8.1 adds `internal/taskjournal` at an 80% minimum and keeps all existing thresholds. The release candidate measures mcpserver 83.7%, OAuth 86.1%, console 83.9%, Brain 80.9%, taskjournal 82.4%, observability 78.8%, tools 74.2% and app 69.8%. React TypeScript, Vitest and Vite build are blocking before Go tests; CodeQL covers JavaScript/TypeScript. Exact allowlist tests reject extra console keys.

P9 release-candidate head `96f7ca15183271772aecbf2d0ac2cceb88e20e5d` passed CI
run `29306099092` and Security Evidence run `29306099088`, including Verify, Race,
Staticcheck, Govulncheck, CodeQL, Dependency Review, Docker build, SPDX SBOM and the
unchanged zero-High/Critical Grype threshold. The dated baseline is merge-ready;
production identity and smoke remain pending until merge and `/brain` persistence.

Workflow policy (always through `go test ./...`): dangerous triggers, permissions,
secrets, mutable versions, missing timeouts, and production actions fail before merge.

Core CI is split into independent blocking verify, CGO race, staticcheck, and
govulncheck jobs so one failure remains attributable. Pinned actionlint validates
workflow expressions/schema, and `cmd/grype-gate` converts the JSON image report into
actionable annotations without lowering the High threshold.

Structured observability added by P7 is also blocking through ordinary tests:

- closed event schema with no free-form map/message/body/params/result/path/target/error;
- internally generated request ids and normalized methods/routes;
- canary tests for prompts, secret-shaped tokens, paths, query values, client ids,
  unknown tool names, and raw errors;
- concurrent line-safe JSONL;
- private 0700/0600 file permissions and bounded one-backup rotation;
- startup failure on invalid/unwritable configuration;
- no public MCP tool, endpoint, exporter, or application.

Core Go:

- formatting check;
- `go test ./... -count=1`;
- `go vet ./...`;
- `go build ./...`;
- `staticcheck`;
- focused Semgrep rules;
- generated catalog/docs consistency.

Authenticated console:

- digest-only bounded session-store tests and constant-time static-token validation;
- exact safe status schema and unchanged MCP/OAuth/catalog integration tests;
- CSP, cookie, method, body-limit, cache, and cross-origin header tests;
- dependency-free embedded asset tests forbidding external origins, analytics, browser
  storage, service workers, `innerHTML`, eval, and WebSockets;
- authenticated `cmd/console-smoke` after deployment, with token read only from the
  environment and no token/session output.

## Main branch or protected merge

Security evidence added by P6:

- CodeQL Go analysis with minimal `security-events: write`;
- pull-request dependency review at moderate severity;
- local Docker build, SPDX JSON SBOM, and blocking high-severity Grype scan;
- versioned finding/remediation evidence under `docs/security-reports/` with package,
  layer, reachability, fix, workflow run, and before/after identity;
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


## P8.1 Console 2.0 gates

- React/TypeScript/Vite builds to fixed embedded assets; no Node service enters production.
- CSP permits same-origin scripts/connect only; source tests forbid inline scripts, external origins, dangerous DOM injection, browser storage, service workers and WebSockets.
- OAuth tests cover state digesting, PKCE S256, exact callback/audience/scope, single-use codes, replay/cross-flow rejection and strict opaque cookies.
- `?key=<valid-token>` must return 401; Authorization bearer recovery and OAuth MCP authorization must remain green.
- Task journal schemas and files are bounded, private, symlink-safe and content-free; stale active state renders disconnected rather than fake autonomy.
- SSE is authenticated and best-effort with durable polling fallback.
- System, payload, observability, Brain graph, security and Edge data use exact nested key allowlists; Brain identifiers are opaque and Edge remains Not paired.
- Production closure requires exact commit, 67 tools, P9 catalog hash, existing catalog/Brain smokes, console OAuth/status/data/tasks/SSE smoke and no new application or pending deployment.


## Console durable live state closure gates

For `console-durable-live-state`, require all of the following before the PR can be considered green:

1. `go test ./...` and coverage report.
2. `go test -race ./...`, `go vet ./...` and production build.
3. Staticcheck, Govulncheck and Actionlint.
4. Frozen-lockfile frontend check, Vitest and Vite production build.
5. OAuth and durable-session restart E2E.
6. legacy JSON migration and journal/event restart E2E.
7. production Docker build.
8. CodeQL, SBOM and Dependency Review.
9. console smoke verifying the published SHA, 85 tools, catalog hash and presentation-only surface.

No gate authorizes merge or deployment.
