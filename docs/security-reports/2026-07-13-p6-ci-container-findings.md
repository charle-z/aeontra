# P6 CI and container security findings — 2026-07-13

## Scope and evidence identity

- Repository: `charle-z/mcp-devbox`
- Commit analyzed before remediation: `112ca8ce06ffdeba570e486a548801ee21692a6f`
- CI run: `29263139285`
- Security Evidence run: `29263139756`
- Staticcheck job: `86861702398`
- Govulncheck job: `86861702481`
- Container job: `86861700073`
- CodeQL job: `86861700230`
- Dependency Review job: `86861732439` (correctly skipped because the event was a push)
- Production deployment checked before remediation: `bn9ehyy686ag4zm5os5cijxl`, finished and serving the analyzed commit.
- Production contract before remediation: healthy, 62 tools, catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

The report records all High/Critical container findings emitted by the tested `cmd/grype-gate`, the reachable Go vulnerability reported by Govulncheck, and the real Staticcheck failures. No finding was ignored, allowlisted, downgraded, or converted to `continue-on-error`.

## Executive result

The observed commit was not eligible to close P6. Verify and Race passed, but Staticcheck, Govulncheck, and the final container vulnerability gate failed. CodeQL passed. Five High container findings were present: three in GNU Wget and two in npm's bundled dependency tree. Govulncheck also proved one standard-library vulnerability reachable from repository call paths.

Step 91 remediates the causes rather than suppressing results:

1. Go is raised from 1.26.4/floating 1.26 to the fixed 1.26.5 release in `go.mod`, every workflow, the production Docker build, and the validation-runner build.
2. The vulnerable standalone GNU Wget package is removed; the healthcheck uses the BusyBox applet already present in Alpine.
3. npm is upgraded in the final image to exact version 12.0.1. Its bundled tree contains `sigstore@5.0.0` and `picomatch@4.0.5`, above the affected ranges.
4. All 25 Staticcheck findings are corrected: three unused declarations are removed and 22 error strings are made idiomatic without changing successful public responses or tool schemas.
5. `internal/workflowpolicy/security_remediation_test.go` prevents regression of the fixed Go version, versioned Go/Alpine base, npm remediation, and Wget removal.

Post-remediation Actions, SBOM, Grype, and production evidence must be appended before this report is treated as final closure evidence.

## Finding inventory

### GO-2026-5856 / CVE-2026-42505 — `crypto/tls`

| Field | Evidence |
|---|---|
| Severity | Govulncheck security finding; workflow blocking |
| Component | Go standard library `crypto/tls` |
| Installed/build version | Go 1.26.4 in `go.mod` and GitHub Actions; Docker used floating `golang:1.26-alpine` |
| Fixed version | Go 1.26.5 |
| Reachability | Confirmed by Govulncheck, not theoretical |
| Repository call paths | `cmd/validation-runner/main.go:65`, `internal/tools/action_plan.go:48`, `internal/mcpserver/server.go:70`, `internal/tools/validation.go:56` |
| Introducing source | Workflow `setup-go` values and Go build stages |
| Final image | The compiled MCP binary and retained Go toolchain were built from the vulnerable release at the analyzed commit |
| Remediation | Pin Go 1.26.5 in `go.mod`, `.github/workflows/{ci,security,fuzz}.yml`, `Dockerfile`, and `Dockerfile.validation-runner` |

Primary references:

- `https://pkg.go.dev/vuln/GO-2026-5856`
- `https://go.dev/doc/devel/release#go1.26.5`

Post-change locations: `go.mod:3`; `.github/workflows/ci.yml:24,61,78,97`; `.github/workflows/security.yml:28,71`; `.github/workflows/fuzz.yml:43`; `Dockerfile:1,23`; `Dockerfile.validation-runner:1`.

### CVE-2026-58469 — GNU Wget

| Field | Evidence |
|---|---|
| Severity | High |
| Package/type | `wget`, Alpine `apk` |
| Installed version | `1.25.0-r3` |
| Fixed version reported by Grype | Not listed |
| Location | `/lib/apk/db/installed` |
| Introducing source | Runtime `apk add` at `Dockerfile` line 31 in commit `112ca8c` |
| Stage/layer | Final runtime stage; package installation layer |
| Present in final image | Yes |
| Runtime reachability | Yes; the Docker healthcheck invoked GNU Wget every probe |
| Remediation | Remove the package and call `busybox wget` explicitly in the healthcheck |

### CVE-2026-58471 — GNU Wget

The package, version, layer, location, final-image presence, reachability, and remediation are identical to CVE-2026-58469. It is retained separately because Grype emitted a distinct vulnerability identifier.

### CVE-2026-58472 — GNU Wget

The package, version, layer, location, final-image presence, reachability, and remediation are identical to CVE-2026-58469. It is retained separately because Grype emitted a distinct vulnerability identifier.

Post-change locations for all three Wget findings: `Dockerfile:31-34` no longer installs GNU Wget; `Dockerfile:59-60` uses the BusyBox applet. The package must be absent from the post-remediation SBOM and Grype report.

### GHSA-52v5-jr5w-gjxr / CVE-2026-48815 — `sigstore`

| Field | Evidence |
|---|---|
| Severity | High |
| Package/type | `sigstore`, npm |
| Installed version | `4.1.0` |
| Fixed version | `4.1.1` |
| Location | `/usr/lib/node_modules/npm/node_modules/sigstore/package.json` |
| Introducing source | Alpine `npm` installed in the final runtime stage |
| Stage/layer | Final runtime `apk add` layer |
| Present in final image | Yes |
| Runtime reachability | Not on the MCP HTTP serving path, but reachable when the retained builder capability runs npm operations that use npm's signing/verification dependency tree |
| Remediation | Install exact `npm@12.0.1`; inspected package contents contain `sigstore@5.0.0` |

Primary advisory: `https://github.com/advisories/GHSA-52v5-jr5w-gjxr`.

### GHSA-c2c7-rcm5-vvqj / CVE-2026-33671 — `picomatch`

| Field | Evidence |
|---|---|
| Severity | High |
| Package/type | `picomatch`, npm |
| Installed version | `4.0.3` |
| Fixed version | `4.0.4` |
| Location | `/usr/lib/node_modules/npm/node_modules/tinyglobby/node_modules/picomatch/package.json` |
| Introducing source | Alpine `npm` installed in the final runtime stage |
| Stage/layer | Final runtime `apk add` layer |
| Present in final image | Yes |
| Runtime reachability | Not on the MCP HTTP serving path, but reachable when npm/tinyglobby processes patterns during authorized builder work |
| Remediation | Install exact `npm@12.0.1`; inspected package contents contain `picomatch@4.0.5` |

Primary advisory: `https://github.com/advisories/GHSA-c2c7-rcm5-vvqj`.

The exact npm remediation starts at `Dockerfile:32`. Node compatibility was checked against Alpine 3.24's `nodejs 24.17.0-r0`; npm 12.0.1 declares support for Node `^24.15.0`. The npm tarball was inspected before selection rather than assuming the top-level npm version fixed its bundled tree.

The first Step 91 PR image build (`Security Evidence` run `29270350078`, container job
`86886191169`) proved that installing npm 12 globally did **not** remove Alpine's
bootstrap npm tree. Grype still found `sigstore@4.1.0` and `picomatch@4.0.3` under
`/usr/lib/node_modules/npm/`, while the fixed npm existed separately under
`/usr/local`. The follow-up remediation therefore runs `apk del npm` after the safe
global installation and cache cleanup. This removes the vulnerable distro copy from
the final image rather than hiding duplicate-package findings.

## Staticcheck findings

Staticcheck job `86861702398` produced 25 blocking findings:

- `U1000`: unused `defaultToolContractVersion` in `internal/mcpserver/catalog.go`;
- `U1000`: unused `(*RepositoryCapability).fileTree` in `internal/tools/context.go`;
- `U1000`: unused `(*CoolifyClient).mountAllowed` in `internal/tools/coolify.go`;
- 22 `ST1005` findings across `internal/tools/platform.go`, `internal/tools/platform_force_deploy.go`, and `internal/tools/validation_runner_platform.go` because returned error strings began with `Coolify`.

The three dead declarations were removed. Only error text was lowercased for ST1005; tool names, schemas, successful output, approvals, environment variables, and the public catalog were not changed.

## Layer and provenance audit

At commit `112ca8c`, `git blame` attributes the vulnerable production-runtime choices as follows:

- `Dockerfile:1` floating build image: original commit `46bef1b`;
- `Dockerfile:23` floating runtime image: commit `f6140e82`;
- `Dockerfile:31` `apk add ... npm wget`: commit `83d76cd4`;
- `Dockerfile:58` healthcheck invoking Wget: original commit `46bef1b`.

This is provenance, not blame assignment. The vulnerabilities became actionable when the 2026-07-13 scanner database and Go vulnerability database identified the affected versions.

## Verification performed before publication

Passed locally on branch `p6-step91-security-remediation`:

- RED regression test failed before implementation and passed after implementation;
- `go fmt ./...`;
- `go test ./... -count=1`;
- atomic coverage generation and `cmd/coverage-gate`;
- `go vet ./...`;
- `go build ./...`;
- `actionlint@v1.7.12`;
- `govulncheck@v1.6.0 ./...` (`No vulnerabilities found`);
- focused `workflowpolicy`, `grypegate`, and `cmd/grype-gate` tests.

Staticcheck cannot initialize its cache in the currently deployed non-root production container because that old image has an unwritable HOME and no general XDG cache setting. The GitHub job already uses a runner-temporary cache and is the authoritative execution environment for this gate. Docker build, SBOM, and Grype also require the ephemeral Actions runner because the public MCP deliberately has no Docker socket.

## Post-remediation and closure evidence

The first repaired image run `29270350078` exposed the duplicate Alpine npm tree.
The follow-up commit removed it; CI run `29270949295` and Security Evidence run
`29270949313` then proved Verify, Race, Staticcheck, Govulncheck, CodeQL, Docker
build, SPDX SBOM, Grype scan, and the unchanged High/Critical gate. That gate reported
zero remaining findings.

GitHub Dependency Graph was subsequently enabled. Final PR commit
`539e4d96c95aedd492ac36b428d4159054e183f4` produced:

- CI run `29272847130`: Verify, Race, Staticcheck, and Govulncheck success.
- Security Evidence run `29272847139`: CodeQL, Dependency Review, and container
  SBOM/vulnerability gate success.

The same commit was fast-forwarded to `main`. Dependency graph update run
`29273109419`, CI push run `29273109759`, and Security Evidence push run
`29273109780` completed successfully. Dependency Review was correctly skipped on
push after passing on the PR.

Production now reports `status=ok`, commit
`539e4d96c95aedd492ac36b428d4159054e183f4`, `tool_count=62`, and catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
The deployment request caused the expected MCP self-restart and the connection ended
before the deployment UUID was returned. The available safe read surfaces confirmed
healthy exact runtime identity but could not recover that UUID; no identifier is
invented. Full closure evidence is in `docs/baselines/2026-07-13-p6.md`.
