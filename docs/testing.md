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

## Direct background-process candidate

The Hito 3A focused matrix runs on Linux because the launcher and PID/start-time
identity are Linux contracts:

```text
go test ./internal/edge ./internal/edgeclient ./internal/mcpserver ./cmd/mcp-edge -count=1
go test ./internal/mcpserver ./internal/app ./internal/integration ./cmd/mcp-catalog-smoke ./docs -count=1
```

It covers closed request/result validation, operation-kind binding, same-request
idempotency, conflicting reuse, no implicit shell, cwd traversal and symlink escape,
secret input rejection, independent concurrent processes, natural zero/non-zero exit,
separate incremental stdout/stderr, output ceilings, split-chunk and private-key
redaction before persistence, TERM/KILL escalation, repeated stop, cross-project
lookup denial, private-log no-follow checks and PID start-time reuse defense. Exact-head CI remains authoritative for
race, Linux packaging modes and the complete release matrix.

Hito 3B extends that matrix with manager close/reopen recovery, live-process reuse
without duplication, offline exit classification, incomplete metadata, foreign owner,
missing process group, unsafe/missing logs, bounded list output, closed signal values,
live-preserving cleanup and systemd `KillMode=process`. Reconciliation runs only on
open or an explicit lifecycle request; there is no polling goroutine or idle busy loop.
The worker regression proves split-stream redaction and an exact non-zero exit receipt
without storing unredacted pipe output in the Edge control process.

Safe checkout synchronization adds focused tests for fixed owner-bound remotes, live
versus fetched remote identity, ahead/behind parsing, no-tag fetch, clean attached
preview, ancestor proof, exact `merge --ff-only`, single-use replay rejection, dirty
tree rejection, malformed plan state, private ownership/modes, and operation-kind
result binding. The focused command is:

```text
go test ./internal/edge ./cmd/mcp-edge ./internal/mcpserver -count=1
```

The first `p15.0.18` real-device pass accepted Hito 3A and exposed two host-specific
gaps that fake runners did not reproduce: the worker and Bubblewrap workload use
separate process groups, and Podman 5.4 returns a bare 64-hex image ID. The corrective
matrix additionally proves that a signal targets the recorded workload group without
killing the receipt-writing worker and that Docker-prefixed and Podman-bare SHA-256
identities canonicalize identically while malformed forms fail closed. See
`docs/baselines/2026-08-03-p15-real-edge-acceptance-fixes.md`.

The `p15.0.19` retry exposed two additional format-boundary regressions. Podman also
returns the container inspect `.Image` identity as bare lowercase hexadecimal during
ownership verification, so that second engine value must pass through the same strict
canonicalizer. Local Git commands deliberately receive an empty credential, and Go's
`strings.ReplaceAll` must not be called with that empty value because it inserts the
replacement between every rune. Focused tests now cover both the second bare identity
and unchanged local output while retaining exact non-empty credential redaction. See
`docs/baselines/2026-08-03-p15-real-edge-acceptance-followup.md`.

The subsequent Hito 3B retry proved log continuity across the managed update but also
exposed a terminal-cleanup gap. A stale journal identity could be classified
`process_identity_changed` while the owner-only worker/child identity artifacts still
resolved to live exact PID/start-tick/process-group tuples. Cleanup trusted the
terminal row and removed metadata even though the worker and Bubblewrap child remained
alive. The focused matrix now proves that reconciliation can restore the exact live
private worker identity before offline classification, and that cleanup counts the
record as active while either the worker or child identity remains live. PID reuse,
foreign ownership, malformed private state and genuinely dead terminal records retain
their fail-closed behavior.

The `p15.0.21` real retry proved that the private worker identity repair preserves a
running process and continuous non-duplicated stdout/stderr across an Edge restart.
It also isolated the remaining signal defect: Bubblewrap's `--new-session` reports an
inner sandbox leader distinct from the outer supervisor returned by `exec.Start`.
Signalling the outer group terminated the supervisor but left the inner Bubblewrap and
workload alive. The new worker regression uses a separate-session helper and fails if
the persisted identity is the launcher rather than the reported leader. The GREEN
matrix reserves Bubblewrap `--info-fd`, accepts only a positive reported child PID,
revalidates its exact owner/start-ticks/PGID tuple before readiness, targets that group,
and keeps `--die-with-parent` bound to the durable worker to close crash-time orphans.

The first `p15.0.22` real start then exposed a readiness race hidden by the original
helper: Bubblewrap writes `child-pid` before `--new-session` necessarily finishes
`setsid`. Immediate `PGID == PID` validation failed closed while the harmless bounded
probe continued and exited normally. The regression now deliberately publishes the
child PID before the delayed `setsid`; RED fails to persist readiness, while GREEN
waits at most two seconds and still requires an unchanged start time, current-user
ownership and `PGID == PID`. The affected Edge/docs matrix passes on Linux.

Signed `p15.0.23` closes the real gate. A fresh process resumed after exactly one
managed Edge restart at the next sequential stdout/stderr record, with no replay. A
public closed `interrupt` terminated the inner sandbox without operator help and
produced known exit code 130. Repeated interrupt and stop remained idempotent;
exclusive cleanup removed the record, the final list was empty, no marked workload
remained and doctor reported ready with an empty journal and `NRestarts=0`.

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
| `internal/mcpserver` | 80% | 82.6% |
| `internal/mcpserver/catalog` | 80% | 85.6% |
| `internal/oauth` | 80% | 85.3% |
| `internal/audit` | 80% | 86.2% |
| `internal/observability` | 70% | 78.4% |
| `internal/telemetry` | 75% | 79.7% |
| `internal/console` | 80% | 84.3% |
| `internal/brain` | 80% | 81.2% |
| `internal/tools` | 70% | 73.9% |
| `internal/app` | 65% | 71.3% |
| `internal/grantadmin` | 55% | 59.6% |
| `internal/workqueue` | 70% | 77.4% |
| `internal/buildspike` | 75% | 82.3% |

The gate fails with an explicit missing package error when a threshold package is
absent, when a profile is malformed, or when a package drops below its minimum.
Official Go coverprofiles may contain synthetic zero-statement records (`0 0`);
the parser accepts and ignores those records while rejecting negative statements or
an impossible executed zero-statement record (`0 1`). A Step 5 regression test
locks this compatibility with `go tool cover`. Output is package-specific and
actionable. Thresholds are deliberately below the current measured value so the gate
detects regression without turning coverage into line-count gaming. P6 will run the same two commands as a blocking CI job and retain
only safe coverage artifacts.

## Hermetic integration matrix — P5 Step 83

The local integration suite validates complete contracts without external services,
real credentials, arbitrary processes, or production traffic:

- **stdio/HTTP catalog parity:** both transports return the same ordered 67-tool
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

## P9 Brain — Step 7 runtime, volume, and remote smoke

Step 7 wires the optional `MCP_DEVBOX_BRAIN_ROOT` contract without changing the
catalog. Runtime tests prove:

- environment parsing accepts only an absolute path and does not reflect invalid input;
- unset Brain preserves the 67-tool catalog and one uniform disabled error;
- configured startup creates private root/trust/cache/Git state, opens FTS5, performs a
  full reindex, and attaches the isolated capability;
- Brain remains outside repository policy roots;
- equal/nested repository overlap, malformed Markdown and an existing Git remote fail
  startup instead of silently disabling the capability;
- runtime close releases the index before audit/observability sinks.

The production Dockerfile now copies `go.sum`, prepares `/brain` for non-root
UID/GID 10001, and declares it as a dedicated volume beside `/repos`. The runbook
locks private modes, curation, backup excluding disposable cache, restore, update,
rollback and troubleshooting. No new process, port, service or application is added.

`cmd/brain-smoke` performs a read-only remote verification of exact commit/catalog,
`brain_index status` and bounded `brain_context`. Its integration test uses a real MCP
HTTP handler and synthetic private note, proving that the output excludes the bearer,
root, slug, title, provenance and body. Coverage is 76.6% for the command.

Coverage after Step 7: `internal/app` 71.3%, Brain 81.2%, tools 73.9%, server 82.6%,
catalog 85.6%; package gates remain green. Production is still P8/62 until the P9 PR,
remote Race/Staticcheck/CodeQL/Dependency Review/container gates and deployment smoke
complete.

### P9 release-candidate evidence

Corrected implementation head `96f7ca15183271772aecbf2d0ac2cceb88e20e5d` passed
CI run `29306099092` (Verify, Race detector, Staticcheck and Govulncheck) and Security
Evidence run `29306099088` (CodeQL, Dependency Review, Docker build, SPDX SBOM and the
unchanged zero-High/Critical Grype threshold). The earlier red Staticcheck result on
`3ae3158` is not release evidence and was not reused.

The dated release-candidate baseline and closure consistency test are part of the P9
tree. Production remains P8/62 until fresh checks for the closure SHA pass, PR #4
merges, `/brain` persistence is configured, and deployment smoke completes.

## P15 development Edge Git follow-up

The private development Git boundary is covered at four layers:

- credential tests prove stdin-only atomic 0600 storage, owner validation, invalid
  token rejection, unsafe-file rejection, and token-free status;
- broker tests prove owner-bound clone, fetch/push URL validation, clean/ancestor
  preconditions, short-lived single-use publication plans, replay rejection, traversal
  rejection, and absence of the token from Git argv;
- launcher tests prove the private actions appear only for a configured `dev`
  workcell and that the provider configuration contains no credential;
- provider tests execute clone, preview and publish over the private requester across
  four model turns, reject injected authority fields before the requester, and retain
  the no-API-key/no-TCP-client source invariant.

On 2026-07-20 the candidate passed under Parrot WSL2 Go 1.26.5: `go test ./...
-count=1`, `go vet ./...`, `go build ./...`, all 19 Node provider tests, packaging
shell syntax, and `git diff --check`. Exact-head GitHub gates and live signed-release
installation remain separate closure requirements.

## Hito 5 official GitHub CLI broker candidate

The first direct GitHub broker slice is deterministic and credential-free in tests:

- CLI-import tests inject a synthetic `gh auth token` reader, verify atomic private
  storage and assert that neither stdout nor errors contain the token;
- broker tests inject the command runner, require the exact fixed repository, pull
  request and Actions `gh api` argv, reject malformed or cross-owner metadata, bound
  responses and expose only closed permission issue codes;
- direct-operation tests bind the request to an existing `linux-workcell`/`dev`
  project and validate restart-safe operation persistence without a model runtime;
- MCP tests prove the public schema accepts only project alias and Edge target and
  that the response omits token, URL, path, header and raw CLI output;
- Debian package contract tests retain the official `gh` dependency for package
  installs. Bundle tests separately prove manifest-v2 rollback compatibility,
  manifest-v3 `github-cli` integrity, fixed safe executable resolution and managed-link
  creation/removal across upgrade and rollback. Updater tests also require inspection
  and retirement of only the two fixed legacy Edge units and propagate any systemd
  failure. The real `p15.0.15` rollback proved that an archive updater must not stop
  the legacy Edge caller that is waiting for it. The subsequent `p15.0.16` attempt,
  after the operator completed a manual process handoff, proved that retaining fixed
  `Conflicts`/`After` directives in the signed managed unit still prevented activation.
  The post-handoff regression therefore requires the managed unit to contain neither
  directive. An active unpackaged legacy unit remains a fail-closed operator migration,
  not a reason to add privileged pre-start logic to the signed service.

  Signed `p15.0.17` then isolated a separate pre-unit failure: the v2 updater service
  ran with `ProtectSystem=strict` and could not create the manifest-v3 managed
  `/usr/local/bin/gh` link. Package contracts now require both update and rollback to
  own exactly `/opt/mcp-devbox`, `/etc/systemd/system` and `/usr/local/bin`; repair
  already had that closed authority. No Edge service receives the added write path.

Real-device acceptance has completed interactive `gh auth login`, safe
`mcp-edge github import-gh` and installation of official release `p15.0.13`. A live
`project_github_status` against an owner-bound private repository remains required
after the v2 bridge and v3 bundled-CLI releases. No test fixture or CI artifact
contains a real account credential.

## Browser harness host-specific acceptance

The hosted workflow still blocks on deterministic source, package, catalog, security,
real Chromium and rootless Podman/PostgreSQL lifecycle checks. It does not attempt to
manufacture the Edge's cgroup authority. GitHub's Ubuntu 22.04 runner is inside a
root-owned `/system.slice` unit and does not expose the user-owned delegated subtree that
the real Edge receives below `/user.slice`.

The workflow writes one bounded TSV record containing schema version, checked-out commit,
tree, `not-reproducible` status, the accepted runtime commit and the exact host
limitation. The record is verified and uploaded. Any different cgroup posture, missing
record, malformed identity or unrelated failure remains red; there is no
`continue-on-error` path.

The publication gate is the owner-controlled Edge evidence. Commit
`c27053c56b6214e52862ead675b874670f322295` ran the exact
`TestProjectBrowserHarnessRealPlaywrightE2E` on `parrot-trusted-linux`, including real
rootless Podman, toolbox limits, Playwright/Chromium, FFmpeg video, Internet, localhost,
upload/download, persistent authentication, artifacts, restart-safe manager reopen,
cancellation and cleanup. A later candidate may reuse that acceptance only when its diff
contains no Edge, server, toolbox or browser-harness runtime change and a fresh real
browser smoke passes on the same Edge. Any runtime change requires a new exact E2E. Source
CI and real-device acceptance are reported separately.

## Rootless PostgreSQL fixture identity

The `Rootless Podman, PostgreSQL and Chromium` gate stages the pinned PostgreSQL
image with the runner's rootful Docker daemon before rootful socket isolation, saves it
as an archive, and loads that archive into the user-owned Podman service. The manifest
config digest is then converted into exactly one local immutable-looking reference:

```text
localhost/p12-postgres-fixture:<64 lowercase hexadecimal characters>
```

Before saving the archive, Docker applies the digest-derived local tag and verifies that
it resolves to the pulled image ID. Rootless Podman then loads the archive and writes a
bounded `images --no-trunc` inventory. A strict parser accepts only full image IDs,
requires exactly one match for the archive config digest, and rejects malformed or
ambiguous output. The service then creates the closed local tag from that exact loaded
ID and verifies it through `podman image inspect` before exporting
`P12_POSTGRES_IMAGE`.

This does not assume that a Docker archive tag survives a remote Podman load. It also
avoids the remote-client `image exists` path, which returned failure after a successful
load. The parser rejects registry names, mutable tags, raw public inputs, uppercase
digests and traversal. The flow remains closed without permitting a network pull or arbitrary image reference.

## Safety rules

- Do not run active DAST against production.
- Do not persist global Go environment changes on the production container.
- Do not add credentials, real tokens, private targets, or source snapshots to test
  corpora or CI artifacts.
- Tests that need time, randomness, HTTP peers, runners, or stores use injected clocks,
  deterministic ids, loopback synthetic servers, and temporary directories.
- A skipped or prerequisite-blocked gate remains visible as blocked; it is never
  silently converted to a pass.


## Console durable live state tests

The console milestone adds deterministic tests for:

- atomic/restart-safe Brain HMAC identity, permissions, secret fallbacks and graph bounds;
- durable session restart, revocation, corruption and raw-cookie absence;
- JSON-to-SQLite task/event migration idempotency and journal restart;
- event retention/quota, exact filters, stable cursors and SSE replay/gap reset;
- real opaque Project/Edge selectors and combined storage budget failure modes;
- React schema v3, precise timestamps, pagination, Last-Event-ID reconnection and cleanup.

Final closure must still run the complete gate matrix in `docs/quality-gates.md` and the production console smoke against the published branch SHA.
