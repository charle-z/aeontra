# Current task — Presentation optimization Hito 2 complete

Authoritative plan: Brain note `mcp-devbox-presentation-optimization`.

## Completion state

Hito 2 — Comparación visual de autoridad is complete on branch `showcase/pixelgrama-evidence`.

Implementation commit: `4d2ffd185d587903bd5eef870ea8f2aeee9380b9` (`Add visual authority mode comparison`).

Hito 1 remains preserved through commits `deb3665e48466b77500f36cf6dbb70a30fd8e4f8` and `388473cc9b515e295416ebaff559ccad23af0070`. Hito 4 remains preserved through commits `69884a2` and `9aefa0e`; its canonical manifest, validator, handler integration and public static resource were not modified.

No branch was published, no pull request was created, nothing was merged, MCP Devbox was not deployed, and Hito 3 has not started.

## Hito 2 result

- Replaced the prose-only authority introduction with a bilingual two-lane visual comparison.
- The broad-authority lane shows a general shell, inherited credentials and environmental access, arbitrary commands, effects beyond the task perimeter, and consequences that are difficult to bound before execution.
- The MCP Devbox lane shows closed-schema tools, pre-authorized repositories/branches/apps/targets, denied paths and secrets, validated commands and parameters, `read-only`/`ask`/`allow`, bound plans, revalidation, bounded output, redaction and audit.
- Added an accessible local-only conceptual selector with exactly three modes:
  - `READ-ONLY`: inspect and diagnose without writes or command execution;
  - `ASK`: work while explicitly approving effects marked reviewable by policy, without claiming every read or safe step stops;
  - `ALLOW`: autonomous work inside administrator-configured authority, explicitly not a free shell.
- The selector distinguishes `mode ≠ plan ≠ human grant`, uses tab/tabpanel semantics, updates `aria-selected`, roving `tabindex`, hidden panels, and Arrow/Home/End keyboard navigation.
- Added the mandatory bilingual warning that reducing authority does not make generated code or every allowed operation inherently safe.
- The comparison uses headings, ordered step numbers, outcomes, borders and selected markers, so meaning does not depend only on color.
- Desktop uses two authority columns and three mode tabs; mobile stacks the authority lanes and collapses the tabs to one column.
- The interaction remains entirely in the browser and adds no server call or authority.

## Files changed

- `internal/landing/assets/index.html`
- `internal/landing/assets/app.css`
- `internal/landing/assets/app.js`
- `internal/landing/assets_test.go`
- `docs/landing/public-showcase.md`
- `docs/public_showcase_test.go`

## Validation results

- Focused `go test ./internal/landing ./docs ./docs/showcase -count=1`: passed.
- Complete Go test suite executed in three bounded groups: all packages passed, including Edge, MCP server, OAuth, policy, tools, workqueue, packaging and profiles.
- `go vet ./...`: passed.
- `go build ./...`: passed.
- `git diff --check`: passed.
- Existing tests confirmed exactly one public `/version` fetch and no new `/mcp`, console, storage, WebSocket, SSE or control-plane capability.
- Existing hero regressions passed, preserving Hito 1.
- A scoped diff confirmed `docs/showcase`, `internal/landing/handler.go` and `internal/landing/handler_test.go` remained unchanged, preserving Hito 4.
- Local Race remains unavailable in this execution profile because CGO is disabled; the exact-head CI race job explicitly enables CGO and remains mandatory before merge.

## Pending work

The next exact milestone is Hito 3 — Demo guiada de solo lectura. Preserve Hitos 4, 1 and 2 unless a real defect is found. Implement only the public read-only Pixelgrama walkthrough using the canonical manifest: request, perimeter, change, validation, external operations and result. It must expose no authority, credentials, private identifiers, audit data, console access or grants. Stop before the cross-cutting mobile/accessibility/CSP review and Hito 5.
