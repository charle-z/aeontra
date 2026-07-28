# Current task — presentation optimization complete

Authoritative plan: Brain note `mcp-devbox-presentation-optimization`.

## Observed implementation state

- PR #70 was merged into `main`.
- PR head: `61fe682228e4d86c53cb3ed168cb77186be6595b`.
- Merge commit: `182a14fe6c64df5651bd25c57a01056ecff446b1`.
- All 16 exact-head checks passed before merge.
- Local `main` and `origin/main` were synchronized and clean at the merge commit.
- The MCP Devbox production application was healthy and `/version` reported exactly `182a14fe6c64df5651bd25c57a01056ecff446b1` before the final closeout.
- The public landing and `/showcase/pixelgrama-evidence.json` both returned HTTP 200.

The live deployment identity must always be read from `/version`; a repository file cannot reliably embed the future merge commit that contains the file itself. Brain records the final post-merge production observation.

## Completed presentation scope

- Hito 4: canonical public Pixelgrama evidence manifest, closed validation, build embedding and safe static serving.
- Hito 1: benefit-first bilingual landing hierarchy.
- Hito 2: visual authority comparison and accessible `read-only`, `ask` and `allow` explanation.
- Hito 3: public read-only Pixelgrama walkthrough grounded in the canonical manifest.
- Cross-cutting review: mobile layout, accessibility, focus isolation, loading state and CSP hardening.
- Product cleanup: the obsolete event delivery document was removed while CubePath attribution remained in the README.

At the start of the final closeout, implementation, CI, merge and production verification were complete; only documentation, continuity and safe neutralization of obsolete event wording remained.

## Final closeout contract update

The managed validation-runner creation preview now defaults to the repository's active product branch, `main`, instead of an obsolete event branch name. The preview contract is version 2. Tool count remains 102; the deterministic current catalog hash is `sha256:477bfd598edec2d8c2e03cea3e13c60cc78f898083138e326e8fed55feb8ca1b`.

This update does not change destination ownership, mount allowlists, approval, plans, isolation, deployment posture or secret handling. Historical dated baselines and Git history remain unchanged.

## Authority model summary

- `read-only`: inspect and diagnose without writes or command execution.
- `ask`: authorized direct writes require an approval round trip before exact execution.
- `allow`: authorized direct writes execute without that approval round trip.

Modes remain separate from exact, expiring and single-use plans and from local human grants. `allow` does not remove repository jails, secret denial, schemas, allowlists, redaction, audit, application restrictions or state revalidation.

## Owner-facing recommendation

The Hito 5 recommendation was delivered directly to the owner in chat. It is intentionally not stored as a repository file, README section, issue, event delivery document or detailed `.agent-memory` artifact.
