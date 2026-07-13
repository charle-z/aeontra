# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p4-l1-hardening`
Deployed base: `main` at `dd055e251c455086ddcb02bc302d9f406b05d6ce`
Working HEAD before Step 76: `fb6b796`

## Current phase

P4 targeted Layer-1 hardening is active and unreleased. P0–P3 are deployed and
production-verified at 62 tools with catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Completed P4 commits:

- `8a3c118` — path-qualified command spoofing blocked.
- `821c252` — workspace-controlled executable resolution blocked.
- `9af06c4` — grant TTL bounds enforced in policy.
- `fe2e903` — pending requests bounded and expired.
- `78a6fff` — documentation synchronization and consistency tests for
  `specs/001-layer-1` and `.specify/memory/constitution.md`.
- `fb6b796` — secret-bearing audit file paths redacted.

## Current work

Step 76 replaces whole-batch allocation with incremental JSON decoding, rejects an
empty JSON-RPC batch, and stops at item 129 with one bounded `-32600` response.
The configured maximum of 128 valid items remains accepted.

## Next safe step

Finish Step 76 gates and commit. Then continue P4 only with another confirmed
Layer-1 security gap and RED test, or begin P4 closure when no material L1 gap remains.
Do not start later product milestones on this branch.
