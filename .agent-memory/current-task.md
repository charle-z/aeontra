# Current task — Presentation optimization Hito 4 complete

Authoritative plan: Brain note `mcp-devbox-presentation-optimization`.

## Completion state

Hito 4 — Public evidence manifest is complete on branch `showcase/pixelgrama-evidence`.

Implementation commit: `69884a2` (`Add canonical Pixelgrama showcase evidence`).
Base and initial HEAD: `main` at `b70efb6c12fe15d7138ea40d033043092c32fc66`.

Hito 1 has not started. No branch was published, no pull request was created, nothing was merged, and MCP Devbox was not deployed.

## Public evidence verified on 2026-07-28

- Canonical production URL: `https://pixelgrama.mcp-devbox-charlez.duckdns.org`.
- `/` responds successfully and deliberately resolves to `/wall`.
- `/wall` responds successfully as the primary public demonstration route.
- `/version` responds successfully with commit `c6eaeae4561c450459cf31b4dc6b4b560abf7cf2`, repository `https://github.com/charle-z/pixelgrama`, and PR `https://github.com/charle-z/pixelgrama/pull/17`.
- Pixelgrama `origin/main` is exactly `c6eaeae4561c450459cf31b4dc6b4b560abf7cf2`; production and source main match.
- Public merged PR/check evidence recorded for PRs 1, 15, 16, and 17.
- Infrastructure is recorded as CubePath with Coolify.

## Files changed by the implementation commit

- `docs/showcase/pixelgrama-evidence.json`
- `docs/showcase/evidence.go`
- `docs/showcase/evidence_test.go`
- `docs/showcase/README.md`
- `docs/documentation-map.md`
- `docs/landing/public-showcase.md`
- `docs/public_showcase_test.go`
- `internal/landing/handler.go`
- `internal/landing/handler_test.go`

## Technical decisions

- `docs/showcase/pixelgrama-evidence.json` is the single canonical evidence source.
- The Go package in `docs/showcase` validates and embeds those exact bytes.
- The existing landing handler serves the static public resource at `/showcase/pixelgrama-evidence.json`.
- No browser-time GitHub query or new production dependency was introduced.
- Missing, empty, malformed, unknown-version, or invalid evidence fails closed during handler construction and CI.
- Validation covers closed fields, required types, HTTPS URLs, lowercase 40-character SHAs, exact Pixelgrama repository and URLs, CubePath/Coolify, successful public checks, sensitive-pattern rejection, private identifier rejection, and separation of historical execution from observed production.
- Exact historical `read-only`, `ask`, or `allow` mode is recorded as `not_publicly_verified`; it was not invented.
- Public results of publication/deployment are separated from private one-time plan artifacts.

## Validation results

- `go test ./docs/showcase ./internal/landing ./docs -count=1`: passed.
- Complete `go test ./... -count=1`: the combined process was terminated by the execution environment after green results through `internal/devaction`; every remaining package was then executed in explicit bounded groups and passed.
- Remaining Edge, lifecycle, integration, MCP server, model turn, OAuth, policy, result store, task journal, telemetry, tools, workflow policy, workqueue, packaging, and profile packages: passed.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check HEAD`: passed before commit.

## Pending work

The next exact milestone is Hito 1 — Nueva jerarquía para la landing. Preserve the Hito 4 manifest and validation unchanged unless a real defect is discovered. Revalidate the branch and current source before editing. Do not begin Hitos 2, 3, or 5 during the Hito 1 execution.
