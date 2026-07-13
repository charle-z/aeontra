# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names so repository-local binaries cannot impersonate allowlisted commands.
- Step 71 `821c252`: resolved commands to canonical absolute paths and rejected relative or workspace-controlled PATH targets.

Current Step 72 candidate:
- moved grant TTL enforcement into the central policy authority instead of trusting only the loopback HTTP adapter;
- zero TTL still selects the documented five-minute default;
- TTLs below one second, negative TTLs and TTLs above one hour are rejected with a dedicated policy error;
- invalid TTL attempts do not consume the pending request, allowing the local human to retry with a valid duration;
- expiration tests now advance the injected policy clock rather than manufacturing an invalid negative-duration grant.

Step 72 verification:
- RED failed because the policy core accepted out-of-bounds TTLs and exposed no TTL error;
- focused grant tests passed for lower/upper bounds, valid retry, default TTL and real expiration;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and `git diff --check` passed.

Next autonomous step: bound and expire pending access requests themselves so an unapproved request id cannot remain valid for the daemon lifetime. Do not publish, merge or deploy P4 without explicit owner approval.
