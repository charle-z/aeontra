# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative/workspace-controlled PATH targets.
- Step 72 `9af06c4`: enforced grant TTL bounds in the central policy core.
- Step 73 `fe2e903`: expired, capped, pruned and deduplicated pending sensitive-read requests.
- Step 74 `78a6fff`: synchronized specs, constitution, capsule, roadmap, README, AGENTS and handoff with tested documentation state.

Current Step 75 candidate:
- audit entries now copy and redact every `Files` element before JSON encoding, in addition to existing Args/Error redaction;
- a token embedded in a file path can no longer become a persistent audit-log secret;
- safe paths remain unchanged and caller-owned slices are not mutated;
- updated security documentation, context capsule, roadmap evidence, documentation-state assertions and latest handoff.

Step 75 verification:
- RED proved a provider token embedded in a path was present in the raw audit JSONL;
- focused audit and documentation tests passed after the fix;
- `go fmt ./internal/audit ./docs`, `go test ./... -count=1`, `go vet ./...`, `go build ./...`, diff review and `git diff --check` passed;
- production smoke remains healthy at P3 commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`, 62 tools, and catalog hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Next after Step 75: continue P4 only with a confirmed Layer-1 security gap and RED test. Do not publish, merge or deploy P4 until phase closure.
