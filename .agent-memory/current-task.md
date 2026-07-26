# Current task

Historical deployed successor truth remains explicit: P8.1 is deployed at
`d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its
successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Current canonical `main` before this branch is synchronized at
`722e63f58355fe154c95dd4c6ae2f2d8118e07ff`, the merge commit for Step 9.
Production identity must not be advanced in documentation until the final Step 10 merge
is deployed and `/version` reports that exact merge.

Branch: `step10-public-landing`.

## Candidate

Step 10 implements a presentation-only unauthenticated `GET /` in new package
`internal/landing`. The existing Go binary embeds HTML, CSS, external local JavaScript,
a static request-path SVG, and an Open Graph SVG. The landing has no console session,
MCP tool path, plan approval, repository/Brain/audit access, private identifiers,
credentials, or control-plane proxy. Its only browser request is the existing safe
public `/version` identity.

The read-only design source was remote branch `landing-public-showcase-design`,
`docs/landing/design-public-showcase.md` and `docs/landing/mockup.html`. Inline
JavaScript, Mermaid runtime source, stale version/catalog/milestone values, and
unrelated claims were deliberately not copied.

P16 Step 7 remains closed. No BuildKit, systemd, AppArmor, runner, quota, or host
posture was changed.

## Deliberate red gates

- `go test ./internal/landing -count=1` failed because `New` and embedded assets did
  not exist.
- `go test ./internal/mcpserver -run PublicLanding -count=1` failed with 404 at `/`
  before registration.
- `go test ./docs -run PublicShowcase -count=1` failed until README, roadmap,
  documentation map and the implementation contract described the new public surface.

## Validation

- `go test ./internal/landing ./internal/mcpserver ./docs -count=1` passed.
- A monolithic fresh suite passed every package through `internal/resultstore` before
  the constrained local process was killed; the explicit remaining group from
  `internal/taskjournal` through `profiles` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `git diff --check` passed.

A later complete run exposed that replacing this file had removed historical P8.1/P9
markers; this version restores them. The subsequent complete `go test ./...` passed.
Remote exact-head gates remain mandatory before merge. Deployment is forbidden before
the merge commit and must be verified by live runtime identity.
