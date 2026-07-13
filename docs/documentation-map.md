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
| What security findings and remediations were verified? | `docs/security-reports/` | New scan finding, remediation, workflow result, or before/after evidence. |
| How is the system operated or recovered? | `docs/runbooks/`, deployment/OAuth/edge guides | Configuration, command, failure mode, update, rollback, or troubleshooting change. |
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
