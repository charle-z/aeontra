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
| `internal/mcpserver` | 80% | 82.7% |
| `internal/mcpserver/catalog` | 80% | 84.4% |
| `internal/oauth` | 80% | 85.3% |
| `internal/audit` | 80% | 86.2% |
| `internal/observability` | 70% | 74.4% |
| `internal/console` | 80% | 84.3% |
| `internal/brain` | 80% | 81.5% |
| `internal/tools` | 70% | 73.3% |
| `internal/app` | 65% | 67.8% |
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

Every job checks out independently, uses Go 1.26.5, has a bounded timeout, and remains
blocking. Local `govulncheck` now completes with no vulnerabilities. Local `staticcheck`
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

## Observed GitHub Actions — P6 Step 90

The first real runs were inspected through GitHub's Actions and Checks APIs rather
than inferred from local success:

- CI run `29260843017` failed before creating jobs. `actionlint@v1.7.12` reproduced
  the schema error: `runner.temp` is unavailable in job-level `env`; the Staticcheck
  cache is now scoped to its execution step and actionlint is a permanent Verify gate.
- Security run `29260848623` showed that CodeQL passed, dependency review correctly
  skipped on a push, and the container scan failed because at least one High or Critical
  vulnerability exists in the image. The severity threshold was not lowered.
- Anchore now writes `grype.json` without ending the action early. The tested
  `cmd/grype-gate` parses that report, emits bounded GitHub annotations containing the
  CVE, package, installed version, fix version, type, and location, then fails at the
  same High threshold. Malformed reports and unknown severities fail closed.

This diagnostic step does not suppress or downgrade findings. The next observed run
will expose the exact vulnerable package so the image can be remediated rather than
ignored.

## Security remediation — P6 Step 91

The corrected runs for commit `112ca8ce06ffdeba570e486a548801ee21692a6f`
created real jobs. CI run `29263139285` proved Verify and Race green while
Staticcheck and Govulncheck failed. Security run `29263139756` proved CodeQL green,
Dependency Review correctly skipped on push, and the container gate failed with five
High findings.

Exact findings and provenance are versioned in
`docs/security-reports/2026-07-13-p6-ci-container-findings.md`:

- reachable `GO-2026-5856` in Go 1.26.4, fixed by Go 1.26.5;
- three GNU Wget High CVEs (`CVE-2026-58469`, `CVE-2026-58471`, and
  `CVE-2026-58472`) introduced in the final `apk add` layer;
- `GHSA-52v5-jr5w-gjxr` in npm's bundled `sigstore@4.1.0`;
- `GHSA-c2c7-rcm5-vvqj` in npm's bundled `picomatch@4.0.3`;
- 25 Staticcheck findings: three dead declarations and 22 capitalized error strings.

The remediation pins Go 1.26.5 across the module, Actions, production image, and
validation-runner build; removes standalone GNU Wget in favor of the existing
BusyBox applet; and installs exact `npm@12.0.1`, whose inspected bundled tree contains
fixed `sigstore@5.0.0` and `picomatch@4.0.5`. A repository policy test locks these
choices. No vulnerability was ignored, allowlisted, downgraded, or hidden.

Local Step 91 verification passes formatting, ordinary tests, atomic coverage and
the package gate, vet, build, actionlint, govulncheck, and focused workflow/Grype tests.
After Dependency Graph activation, PR CI run `29272847130` and Security Evidence
run `29272847139` passed every required job, including Dependency Review and the
zero-High/Critical container gate. Fast-forward push runs `29273109759` and
`29273109780` also passed; Dependency Review was correctly skipped on push after
its successful PR execution. Production serves exact commit
`539e4d96c95aedd492ac36b428d4159054e183f4` with 62 tools and the unchanged hash.
P6 closure evidence is versioned in `docs/baselines/2026-07-13-p6.md`.

## Structured observability — P7

P7 adds `internal/observability` with a closed JSONL schema and a 70% package
coverage minimum. The measured local baseline is 74.4%. Tests cover:

- mode/default/range/path validation and flag-over-environment precedence;
- one-record JSON encoding, concurrent writes, joined multi-writer failures, and a
  failure counter that stores no raw error text;
- private 0700 directories, 0600 active/backup files, and one bounded `.1` rotation;
- internally generated request ids shared across HTTP and JSON-RPC events;
- normalized lifecycle, route, method, public tool, outcome, status, duration, and
  closed error-class fields;
- canaries containing prompts, bodies, params, paths, targets, URLs, query tokens,
  client-controlled request ids, unknown tool names, and secret-shaped strings;
- absence of new tools, endpoints, exporters, collectors, dashboards, or applications.

Local Staticcheck remained blocked before analysis by the deployed non-root
container's unwritable default cache path; the GitHub runner supplied a writable cache.
The first P7 push run `29280567173` passed Verify, Race, and Govulncheck but
Staticcheck job `86920444713` found the obsolete `callTool` wrapper (U1000).
The wrapper was removed without suppression. Corrective CI run `29281156750`
then passed Verify, Race, Staticcheck, and Govulncheck. Security Evidence run
`29281156767` passed CodeQL and the Docker/SBOM/zero-High-Critical gate;
Dependency Review was correctly skipped on push. Production serves exact commit
`d1309ed08db0170e5165f78bf406e94cfa56cc11` with 62 tools and the unchanged
hash, and its JSONL logs were inspected for the closed data contract. Full evidence is
versioned in `docs/baselines/2026-07-13-p7.md`.

## Authenticated dark console — P8

P8 adds `internal/console` with an 80% package coverage minimum and a measured
84.3% local baseline. Ordinary and integration tests cover:

- digest-only opaque sessions, eight-hour TTL, 128-session cap, expiry, revocation,
  collision retry, and restart-only persistence;
- static-token form login, OAuth/bearer bootstrap, logout, cookie scope/security, and
  exact method/content-type/body-size failures;
- authenticated assets/status, exact safe status keys, unchanged MCP authorization,
  and unchanged 62-tool catalog/hash;
- CSP, frame/referrer/content-type/permissions/cache/cross-origin headers on pages,
  JSON, assets, errors, and redirects;
- canaries for tokens, cookies, query secrets, paths, prompts, targets, identities,
  and observability leakage;
- embedded assets with no CDN, analytics, remote font, npm runtime, service worker,
  browser storage, `innerHTML`, WebSocket, or JavaScript-readable cookie;
- `cmd/console-smoke`, which reads the existing token only from the environment,
  validates login/session/status/headers/commit/catalog, and prints no secret/cookie.

PR #2 final runs `29290411676` and `29290411679` passed Verify, Race,
Staticcheck, Govulncheck, CodeQL, Dependency Review, Docker/SBOM, and the unchanged
High/Critical gate. Post-merge runs `29290609147` and `29290609178` also passed;
Dependency Review correctly skipped on push. Production serves
`605a56d48a495f3c8a2ce62471223187ef2f5685`, console-smoke passed, and safe logs
show only 303/200 `route=console` events. Full evidence is versioned in
`docs/baselines/2026-07-13-p8.md`.

## P9 Brain — Step 4 disposable SQLite FTS5 index

`internal/brain` has an 80% package gate and a measured 81.5% Step 4 baseline.
`modernc.org/sqlite@v1.53.0` is now the second deliberate direct dependency. It runs
with `CGO_ENABLED=0` inside the existing process and adds no service, listener, port,
container, credential, queue, model, or worker.

Step 4 tests cover the complete note/jail/Git behavior plus:

- private mode-0600 `.cache/brain.db` creation and symlink/broad-permission rejection;
- FTS5/schema/integrity probes and exact schema version;
- bounded full rebuild from strict Markdown truth inside one transaction;
- atomic preservation of the previous snapshot when a rebuild source is malformed;
- BM25 search over title/body with client input converted to quoted plain-text terms;
- query/top-k/term/excerpt/response/backlink/note-count/aggregate-byte limits;
- deterministic forward links, backlinks, broken-link counts, and status metadata;
- manual secret redaction before indexing and proof the raw canary is absent from the
  SQLite file and search output;
- incremental index updates tied to successful working-note writes;
- source/index rollback when the subsequent local Git commit fails;
- disposable cache deletion followed by equivalent full reconstruction;
- concurrent search/write/reindex behavior and safe close/reopen;
- invalid lifecycle, closed-index, unsafe cache, unexpected source entry, invalid
  context/query/slug, and UTF-8 truncation paths.

Both Brain fuzz targets pass. Local Staticcheck remains blocked before analysis by the
production container's unwritable cache path, and local Race remains blocked because
CGO is disabled. Those gates remain blocking and runner-authoritative for the P9 PR.

## Safety rules

- Do not run active DAST against production.
- Do not persist global Go environment changes on the production container.
- Do not add credentials, real tokens, private targets, or source snapshots to test
  corpora or CI artifacts.
- Tests that need time, randomness, HTTP peers, runners, or stores use injected clocks,
  deterministic ids, loopback synthetic servers, and temporary directories.
- A skipped or prerequisite-blocked gate remains visible as blocked; it is never
  silently converted to a pass.
