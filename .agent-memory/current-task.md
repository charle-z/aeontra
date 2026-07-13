# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

Completed:
- Step 70 `8a3c118`: rejected path-qualified, drive-qualified and whitespace-disguised executable names so repository-local binaries cannot impersonate allowlisted commands.

Current Step 71 candidate:
- replaced direct PATH execution with a trusted executable resolver bound to the configured workspace roots;
- the resolver requires an absolute lookup result, canonicalizes symlinks and rejects any executable located inside a configured workspace root;
- a hostile PATH entry pointing bare `git` or `go` at `/repos/...` can no longer redirect execution to repository-controlled code;
- the runner executes the canonical absolute path rather than re-looking up the bare name;
- legitimate system executables outside the workspace and sibling-prefix paths remain allowed.

Step 71 verification:
- RED failed because trusted resolution and workspace-executable errors did not exist;
- focused resolver tests passed for workspace paths, relative PATH results, sibling paths and symlink targets;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and `git diff --check` passed.

Next autonomous step: enforce grant TTL bounds in the policy core itself so direct/internal callers cannot create negative or overlong grants despite the HTTP adapter validation. Do not publish, merge or deploy P4 without explicit owner approval.
