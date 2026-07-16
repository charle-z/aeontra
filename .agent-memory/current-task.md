# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published validation SHA: `2bda26d4382feca7b9367a068dfbeff917e5538d`
Validated implementation tree: `e8862ee9229ec8a98237251de6d3272e3f72ee1e`
Base: `01fde5067752ab1c43424d2d54f9afd914617ba5`
PR: `https://github.com/charle-z/mcp-devbox/pull/13` (draft)

## Historical deployed state

P9 Brain was deployed before its successor. P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. P11.2 does not alter production.

## P11.2 validated implementation

The Docker-only relay workspace mismatch is fixed in `d3d57668504e1d38edfee7baf9f18379e37f205f`. The real OpenCode 1.18.1 remote integration passed ten consecutive local runs with four turns, read/grep/edit/bash, changed fixture digest, green tests, completed runtime, large request reference and zero duplicates.

Exact-tree GitHub evidence is green:

- pull-request E2E `29540554711`: Distributed OpenCode E2E and Bubblewrap host isolation passed;
- duplicate push E2E `29540553465`: both jobs passed;
- CI `29540554731`: passed;
- Security Evidence `29540554743`: passed.

All generated reports reference tree `e8862ee9229ec8a98237251de6d3272e3f72ee1e`. The current candidate catalog has 78 tools and hash `sha256:9a20218d912bd2f6f42a254145d97c976cfcdd581f89340d563c1642e03318ed`.

## Final candidate documentation and local gates

The dated baseline `docs/baselines/2026-07-16-p11_2.md` records the green architecture, commits, catalog, pinned versions, A/B/C measurements, 6.617088 ms Bubblewrap startup median, reports, Parrot guide, residual risks and rollback. README, AGENTS, capsule, design, documentation map and roadmap now identify the 78-tool P11.2 release candidate while preserving historical 67/71-tool evidence.

Final-tree local gates completed:

- `go fmt ./...`;
- `go test -p 1 ./... -count=1`;
- atomic coverage plus package thresholds;
- `go vet ./...`;
- `go build ./...`;
- Staticcheck `v0.7.0`;
- Govulncheck `v1.6.0` with no vulnerabilities;
- Actionlint `v1.7.12`;
- 13 Node provider contract tests;
- real provider/driver large-request and restart integrations, five repetitions each;
- remote provider normal, restart and four-turn integrations, three repetitions each.

The local race command could not start because this VPS has no `gcc`; this is an environment limitation, not a test failure. Race, no-cache Docker build, SBOM/container scan, CodeQL and Dependency Review remain mandatory on the exact final GitHub SHA.

All temporary diagnostics, helpers, downloaded reports and scratch directories have been removed.

## Immediate objective

Create the canonical commit `Step 8: isolate OpenCode runtime with Bubblewrap`, record its exact SHA/tree, publish without force, update PR #13, and mark ready only after every mandatory final-SHA check is completed and green.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
