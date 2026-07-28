# Current task — Presentation optimization Hito 3 complete

Authoritative plan: Brain note `mcp-devbox-presentation-optimization`.

## Completion state

Hito 3 — Demo guiada de solo lectura is complete on branch `showcase/pixelgrama-evidence`.

Implementation commit: `9c661d71722dcf5258a68dbf275b702a9c07edd9` (`Add read-only Pixelgrama walkthrough`).

Hito 2 remains preserved through commits `4d2ffd185d587903bd5eef870ea8f2aeee9380b9` and `e3ffec10ba31703280cfef509e2ad3cc29ded8bd`. Hito 1 remains preserved through `deb3665e48466b77500f36cf6dbb70a30fd8e4f8` and `388473cc9b515e295416ebaff559ccad23af0070`. Hito 4 remains preserved through `69884a2` and `9aefa0e`; its canonical manifest, Go validator, handler integration and public resource were not modified.

No branch was published, no pull request was created, nothing was merged, MCP Devbox was not deployed, and Hito 5 has not started.

## Hito 3 result

- Added a bilingual six-step public walkthrough: request, perimeter, change, validation, external operations and result.
- The hero's primary proof action now opens the guided demo; the raw canonical manifest remains linked from the demo.
- The demo fetches `/showcase/pixelgrama-evidence.json` exactly once from the same origin. `/version` remains the only other browser fetch.
- The browser validates schema version, exact Pixelgrama repository and branch, HTTPS URLs on the two permitted public hosts, PR SHAs and successful checks, the required `/`, `/wall` and `/version` observations, CubePath/Coolify, the `not_publicly_verified` authority posture, operation evidence and non-empty perimeter entries.
- Rendering uses `textContent`, `createElement` and validated links; it does not use `innerHTML`, browser storage, cookies, WebSockets, SSE, GitHub API calls or private routes.
- Request and PR purpose text are shown verbatim from the public manifest. PR links include the public Files changed view so file details are not duplicated into a second source.
- The exact historical tool list and the historical `read-only` / `ask` / `allow` mode are explicitly marked as unpublished instead of inferred.
- External-operation evidence distinguishes direct operations from plan-protected operations and explains `ask` versus `allow` without claiming a historical mode.
- The result step displays production, `/wall`, `/version`, the observed production commit, source-main commit, verification date and CubePath + Coolify; it explicitly distinguishes historical PR heads from current production observation.
- The demo is presentation-only: no tools, console, plan approval, grants, credentials, repositories, private identifiers or audit data are exposed.
- Desktop uses a two-column six-step grid; mobile stacks it in source order. Step numbers, headings, boundaries and outcomes keep meaning independent of color.
- Failure is generic and does not retry GitHub or expose private diagnostics.

## Files changed

- `internal/landing/assets/index.html`
- `internal/landing/assets/app.css`
- `internal/landing/assets/app.js`
- `internal/landing/assets_test.go`
- `docs/landing/public-showcase.md`
- `docs/public_showcase_test.go`

## Validation results

- Focused `go test ./internal/landing ./docs ./docs/showcase -count=1`: passed.
- Complete Go test suite passed across bounded groups. One combined Edge group was killed by the executor before returning results; `internal/edge`, `internal/edgeclient`, and all remaining packages from that group were rerun separately and passed.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check`: passed.
- Existing security regressions confirmed exactly two same-origin public fetches, no remote fetch, no new `/mcp` or `/console` capability, and continued CSP compatibility through `connect-src 'self'`.
- A compatibility regression loads the real embedded manifest and confirms its safe GitHub fragment is accepted by browser URL validation.
- A scoped diff confirmed `docs/showcase`, `internal/landing/handler.go`, and `internal/landing/handler_test.go` remained unchanged.
- `node --check` was unavailable through the command allowlist, and the optional L3 sandbox backend is not configured. No validation was claimed from either. JavaScript was reviewed directly and is covered by asset regressions; exact-head CI remains mandatory before merge.
- Local Race remains unavailable because CGO is disabled in this execution profile; exact-head CI explicitly enables it.

## Pending work

The next exact stage in the authoritative order is the cross-cutting review of mobile, desktop, accessibility and CSP for Hitos 4, 1, 2 and 3. Preserve the completed milestones, fix only demonstrated presentation defects, execute the relevant regressions and commit locally. Stop before Hito 5 — Reorganización del issue público.
