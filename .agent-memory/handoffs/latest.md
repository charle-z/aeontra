# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p4-l1-hardening`
Deployed base: `main` at `dd055e251c455086ddcb02bc302d9f406b05d6ce`
Working HEAD before Step 75: `78a6ffffd1ce7807ea45d7bc0955e310a9738faa`

## Current phase

P4 targeted Layer-1 hardening is active and unreleased. P0–P3 are deployed and
production-verified at 62 tools with catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Completed P4 commits:

- `8a3c118` — path-qualified command spoofing blocked.
- `821c252` — relative/workspace-controlled executable resolution blocked.
- `9af06c4` — grant TTL bounds enforced in policy.
- `fe2e903` — pending requests expired, capped, pruned, and deduplicated.
- `78a6fff` — documentation synchronization across `specs/001-layer-1`,
  `.specify/memory/constitution.md`, capsule, roadmap, README, AGENTS, and handoff.

## Current work

Step 75 redacts every audit `Files` entry in addition to args and errors. The RED
test proved that a token embedded in a path was persisted before the fix. The logger
now copies the caller slice and redacts each path before JSON encoding, preserving
safe paths and caller-owned input.

## Next safe step

Finish Step 75 gates and commit. Then continue P4 only with another confirmed
Layer-1 security gap and RED test. Do not start console, profiles, asset broker, or
edge work on this branch. Do not publish, merge, or deploy P4 before closure audit.
