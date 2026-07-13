# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative/workspace-controlled PATH targets.
- Step 72 `9af06c4`: enforced grant TTL bounds in the central policy core.
- Step 73 `fe2e903`: expired, capped, pruned and deduplicated pending sensitive-read requests.

Current Step 74 candidate — documentation synchronization:
- marked `specs/001-layer-1` completed/evolved and checked T01-T17 with preserved historical Layer-1 requirements;
- updated the Layer-1 plan to distinguish the original MVP layout from the deployed P0-P3 architecture;
- amended `.specify/memory/constitution.md` with current phase discipline, trusted executable/grant invariants, release gates, and evidence-based documentation rules;
- replaced the stale July 1 handoff with the exact P4 restart point;
- updated `docs/context-capsule.md` from merge-ready P3 to deployed P3 and active P4, including Steps 70-73 behavior;
- added an explicit implemented/in-progress/planned status snapshot to `docs/product-roadmap.md`;
- added `docs/documentation-map.md` defining source hierarchy, status vocabulary and per-step/per-phase update rules;
- synchronized README, AGENTS and the P3 closure test;
- added documentation consistency tests covering specs, constitution, capsule, roadmap, handoff and the documentation map;
- historical dated baselines were intentionally not rewritten.

Step 74 verification:
- RED exposed unchecked completed tasks, original stdio-only scope presented as current, a constitution frozen to the first L1 session, deployed P3 presented as unmerged, no roadmap status snapshot, and a stale handoff;
- final source-of-truth documents were read back after synchronization;
- `go fmt ./docs`, `go test ./docs -count=1`, `go test ./... -count=1`, `go vet ./...`, `go build ./...`, staged diff review and cached diff checks passed;
- production smoke remains healthy at P3 commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`, 62 tools, and catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Next after Step 74: continue P4 only with a confirmed Layer-1 security gap and RED test. Do not publish, merge or deploy P4 until phase closure.
