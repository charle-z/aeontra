# Current task — product-focused presentation PR

PR #70 improves MCP Devbox as a product. The public landing explains the product, its configurable autonomy, authority boundaries, and one public end-to-end application proof. Public-facing documentation no longer contains event submission material or event-specific promotion.

## Authority modes in plain language

- `read-only`: inspect and diagnose; writes and command execution are denied.
- `ask`: authorized work is available, but a direct write action first returns an approval-required result and must be repeated exactly with approval.
- `allow`: authorized direct work executes without that approval round trip.

Plans remain separate from modes. Publication, pull requests, merge, deployment, repository creation and similar consequential external operations still use exact, expiring, single-use plans with state revalidation. `allow` removes unnecessary pauses where policy permits; it does not remove repository jails, secret denial, schemas, allowlists, redaction, audit or target restrictions.

## Current product changes

- canonical public evidence manifest for one end-to-end application proof;
- benefit-first bilingual landing;
- visual comparison between broad ambient authority and bounded tools;
- accessible `read-only`, `ask`, and `allow` selector;
- read-only guided walkthrough;
- mobile, accessibility and CSP hardening;
- README continues to state that production is hosted on CubePath.

## Cleanup

- removed the obsolete event submission Markdown file;
- removed its test contract;
- replaced event-specific roadmap, mockup and release wording with product-demonstration wording;
- removed event naming from current public docs, handoffs and baseline metadata;
- retained one legacy internal validation-runner branch literal because it is part of a frozen historical MCP catalog contract; changing it altered the catalog hash and failed compatibility tests;
- retained historical Git commits without rewriting history.

## Publication state

Branch: `showcase/pixelgrama-evidence`.
Pull request: #70 into `main`.
Do not merge or deploy until every exact-head check is complete and green. After green checks, merge, synchronize `main`, deploy only MCP Devbox through the authorized Coolify application, and verify the exact production commit.
