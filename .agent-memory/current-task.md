# Current task — presentation product improvements on PR #70

Authoritative implementation branch: `showcase/pixelgrama-evidence`.

## Owner correction

The presentation work must remain a product improvement for MCP Devbox, not a Cubethon-specific rewrite. The 700-character opening rule was an artificial presentation contract and is not a product or runtime requirement.

The changes to `docs/cubethon-2026-q3-submission.md` and `docs/p12_parrot_production_test.go` were restored exactly from `origin/main`, so PR #70 no longer changes the Cubethon issue source or adds the 700-character test. Any future issue wording should be delivered as chat output unless the owner explicitly asks to version it in the repository.

## Product framing

- MCP Devbox remains the product.
- Pixelgrama remains one public end-to-end proof used by the landing demo.
- The Pixelgrama evidence manifest does not rename or redefine MCP Devbox.
- The branch name is temporary Git workflow metadata and is not a public product name.
- No new runtime restriction was introduced by the removed 700-character contract; it only constrained documentation text.

## Current gate

Run focused tests, commit the correction, publish the updated exact head to PR #70, and wait for exact-head checks. Do not merge or deploy until all checks are green.
