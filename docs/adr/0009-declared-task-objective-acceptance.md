# ADR 0009 — Declared task objective acceptance

Status: Accepted for implementation

Date: 2026-10-07

## Context

Runtime completion, committed changes and a successful test command are useful facts,
but none alone establishes that a requested objective was met. Existing tasks retain
those facts separately and leave acceptance pending. A deterministic evaluator needs
an explicit contract; inspecting final model prose cannot supply one.

## Decision

Extend the existing task store and status reconciliation, without another scheduler,
database, service or model provider. An optional version-1 objective contract declares
that the pinned operator-owned test profile evaluates each worker goal and specifies
minimum commits, minimum changed paths and whether a clean tree is required.
Zero minima and dirty trees are valid. Existing and contractless tasks keep manual
review semantics. The profile must contain meaningful goal-specific checks; declaring
a generic suite does not prove requirements that the suite never tests.

Acceptance requires terminal successful runtime evidence, exact passing test evidence
and matching current source criteria. Every worker must pass for task acceptance.
The immutable receipt binds the goal hashes, profile, test receipt and source facts.
It survives managed cleanup; until cleanup, live state must still validate. The
receipt grants no publication, merge, deployment or host authority.

SQLite schema 4 adds contract and receipt columns transactionally with empty legacy
defaults. Older binaries reject schema 4 before operating. Before production rollout,
back up the consistent state volume. A rollback to a schema-3 binary requires the
pre-migration backup; do not lower user_version or discard new acceptance records.

## Validation

Cover absent/unknown criteria, version bounds, profile requirements, immutable replay,
goal and test receipt substitution, restart, schema-3 migration, dirty read-only work,
partial multiworker evidence, stale source and cleanup-marker recovery. Source tests,
CI, production deployment and real Edge acceptance are separate evidence.
