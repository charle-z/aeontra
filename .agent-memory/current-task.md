# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names so repository-local binaries cannot impersonate allowlisted commands.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative or workspace-controlled PATH targets.
- Step 72 `9af06c4`: enforced grant TTL bounds in the central policy core.

Current Step 73 candidate:
- pending secret-read requests now expire after 15 minutes instead of remaining approvable for the daemon lifetime;
- the policy stores at most 256 simultaneous pending requests and prunes expired requests/grants before accepting new work;
- repeated requests for the same exact path and raw posture reuse the existing request id, preventing approval-notification spam without merging normal and raw authority;
- approving an expired request returns a dedicated error and consumes that expired request;
- expired entries no longer block new requests after pruning.

Step 73 verification:
- RED failed because pending request expiry, request bounds, deduplication and dedicated errors did not exist;
- focused policy tests passed for expiry, capacity, pruning, duplicate reuse and raw/non-raw separation;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, diff review and `git diff --check` passed.

Documentation audit found material drift in `specs/001-layer-1`, `.specify/memory/constitution.md`, the context capsule and roadmap status. Step 74 will synchronize those sources with implemented/deployed P0-P3 and current P4 without marking unverified work complete.

Do not publish, merge or deploy P4 without explicit owner approval.
