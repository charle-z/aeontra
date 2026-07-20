# Documentation map and update rules

This file explains which document answers which question and when it must be updated.
The repository is the source of truth; chat history is not.

## Source hierarchy

| Question | Primary source | Update trigger |
|---|---|---|
| What security/build rules may never be weakened? | `.specify/memory/constitution.md` and `AGENTS.md` | Principle, process, or authority-boundary change. |
| What is deployed and what is active now? | `docs/context-capsule.md` | Every phase release, rollback, production verification, or active-phase change. |
| What exact step is being worked on? | `.agent-memory/current-task.md` | Before and after every numbered step. |
| Where can another session resume? | `.agent-memory/handoffs/latest.md` | Meaningful checkpoint, phase closure, or blocking failure. |
| What did the original Layer 1 MVP promise? | `specs/001-layer-1/` | Historical corrections only; completed evidence must remain checked. |
| What is planned versus implemented? | `docs/product-roadmap.md` | Milestone status change or roadmap decision. |
| Why was an architecture decision made? | `docs/adr/` | Accepted/replaced architecture decision. Do not rewrite accepted history. |
| What was true at a phase closure? | `docs/baselines/` | Create a new dated baseline; do not rewrite old evidence. |

Current P8 closure evidence: `docs/baselines/2026-07-13-p8.md` plus
`docs/p8_closure_test.go`. P9 architecture is governed by `specs/006-brain/` and
`docs/adr/0003-p9-markdown-truth-sqlite-fts5-cache.md`; it must not rewrite P8 evidence.
P9 release-candidate evidence is `docs/baselines/2026-07-14-p9.md` plus
`docs/p9_closure_test.go`. It records merge-ready checks honestly while production
remains P8 until merge, persistent-volume setup, deployment and smoke.
The P9 resource invariant is no resident service. Runtime setup, curation, backup,
restore, update, rollback, and troubleshooting are governed by
`docs/runbooks/brain-operations.md`.
P8.1 release-candidate evidence is `docs/baselines/2026-07-14-p8_1.md` plus
`docs/p8_1_closure_test.go`. It records the React Neo-BIOS UI, console OAuth migration,
query-key removal, durable task journal, SSE and exact safe data schemas while
production remains the tagged P9 baseline until merge and deploy. P8.1 production closure
is recorded separately in `docs/baselines/2026-07-14-p8_1-production.md`;
the historical release-candidate baseline is not rewritten.
P11 release-candidate evidence is `docs/baselines/2026-07-15-p11.md` plus
`docs/p11_candidate_test.go`; the focused review is
`docs/security-reports/2026-07-15-p11-edge-review.md`. These documents preserve the
historical 67-tool P8.1/P9 and 71-tool P11 evidence. P11.2 relay/isolation evidence
is `docs/baselines/2026-07-16-p11_2.md` plus `docs/p11_2_closure_test.go`;
the current release-candidate contract has 85 tools.
P12 Trusted Linux Workcell candidate behavior and operations are governed by
`docs/linux-workcell.md`, `docs/edge-workcells.md`, and `profiles/htb-linux-v1.md`.
P12 is merged, deployed, paired, and validated on Parrot WSL2. Historical candidate
evidence remains in `docs/baselines/2026-07-18-p12.md`; real-host production evidence
is recorded separately in `docs/baselines/2026-07-18-p12-parrot-production.md`.
The canonical onboarding procedure is `docs/install-opencode-edge-parrot.md`;
authorized HTB room setup and local credential handles are documented in
`docs/htb-lab-workflow.md`.
P15 signed release identity, fixed component layout and fail-closed compatibility
codes are governed by `docs/edge-bundles.md`.
P15 one-time Debian installation, onboarding, migration, update, repair and rollback
are governed by `docs/install-edge-parrot-p15.md`.
| What security findings and remediations were verified? | `docs/security-reports/` | New scan finding, remediation, workflow result, or before/after evidence. |
| How is the system operated or recovered? | `docs/runbooks/`, deployment/OAuth/observability/console/edge guides | Configuration, command, failure mode, update, rollback, or troubleshooting change. |
| What tools and contracts are public? | `docs/tools.md` plus generated catalog tests | Tool name/schema/description/alias/annotation/workflow change. |

## Status vocabulary

Use these terms consistently:

- **Deployed:** merged into `main`, deployed, health checked, and runtime identity verified.
- **Complete / merge-ready:** implementation and closure gates passed, but not yet deployed.
- **In progress:** active branch has unfinished or unreleased work.
- **Planned:** accepted roadmap intent with no completion claim.
- **Not started:** no implementation branch or verified artifact exists.
- **Validation pending:** implementation/docs may exist, but required environment or
  device testing has not happened. This is mandatory wording for untested PC/WSL edge
  flows.

## Per-step documentation rule

Every numbered implementation step must:

1. Update `.agent-memory/current-task.md` with the candidate state and verification.
2. Update behavior/operations docs when the step changes an external or security
   contract.
3. Update `.agent-memory/handoffs/latest.md` when the restart point materially changes.
4. Run documentation consistency tests before commit.

## Per-phase closure rule

A phase cannot be called merge-ready until it has:

- a dated baseline under `docs/baselines/`;
- updated capsule, roadmap status, README/AGENTS when architecture changed;
- current specs/tasks or a new spec for the next product surface;
- setup, configuration, permissions, validation, update, rollback, and troubleshooting
  documentation for operational components;
- full tests, quality gates, diff/commit/file audit, and production smoke against the
  still-deployed previous baseline.

After deployment, update the capsule and phase closure tests from **merge-ready** to
**deployed**. Historical baselines remain unchanged.
