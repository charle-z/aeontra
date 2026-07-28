# Current task — Presentation optimization Hito 1 complete

Authoritative plan: Brain note `mcp-devbox-presentation-optimization`.

## Completion state

Hito 1 — Nueva jerarquía para la landing is complete on branch `showcase/pixelgrama-evidence`.

Implementation commit: `deb3665e48466b77500f36cf6dbb70a30fd8e4f8` (`Lead landing with bounded autonomy proof`).

Hito 4 remains preserved through commits `69884a2` and `9aefa0e`; its canonical manifest and validator were not modified.

No branch was published, no pull request was created, nothing was merged, MCP Devbox was not deployed, and Hito 2 has not started.

## Hito 1 result

- Replaced the component-first boot summary with a benefit-first summary: real infrastructure, no free shell, narrow tools, configurable autonomy, and Pixelgrama proof.
- Reworked the first landing section so it explains, in this order, the excessive-authority problem, MCP Devbox's bounded-tool solution, the owner's autonomy choices, and the public Pixelgrama result.
- Added exactly three primary actions: canonical Pixelgrama evidence, authority model, and MCP Devbox repository.
- Moved implemented/experimental/planned capability detail below the hero rather than presenting it before the value proposition.
- Added a public read-only Pixelgrama proof panel linking `/wall` and `/version`, while stating that the page grants no tool, console, or credential authority.
- Kept the landing bilingual, keyboard accessible, responsive at the existing 760 px and 420 px breakpoints, and aligned with the square VGA/BIOS visual language.
- Updated metadata and the embedded social card to use the same benefit-first message.
- Updated `docs/landing/public-showcase.md` and added regression tests for hierarchy, exact bilingual content, exactly three actions, responsive CSS, startup messaging, and valid social SVG.

## Files changed

- `internal/landing/assets/index.html`
- `internal/landing/assets/app.css`
- `internal/landing/assets/social-card.svg`
- `internal/landing/assets_test.go`
- `docs/landing/public-showcase.md`

## Validation results

- Focused `go test ./internal/landing ./docs/showcase ./docs -count=1`: passed.
- Complete Go test suite executed in three bounded groups: all packages passed, including Edge, MCP server, OAuth, policy, tools, workqueue, packaging, and profiles.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check`: passed.
- Exact diff check confirmed the Hito 4 manifest and validator remained unchanged.
- A focused race invocation did not start because this execution profile has `CGO_ENABLED=0`; this is an environment limitation, not a test failure. The existing CI race job explicitly enables CGO.
- Existing MCP server tests covering the public landing and unchanged `/mcp`, `/console`, `/version`, and OAuth routing passed as part of the suite.

## Pending work

The next exact milestone is Hito 2 — Comparación visual de autoridad. Preserve Hitos 4 and 1 unless a real defect is found. Implement only the broad-shell-versus-MCP-Devbox comparison, the conceptual `read-only` / `ask` / `allow` selector, and the mandatory non-absolute-safety statement. Stop before Hito 3.
