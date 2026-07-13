# P4 targeted Layer-1 hardening

Status: in progress on branch `p4-l1-hardening` from deployed `main` commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`.

P3 release verification:
- feature branch published and `main` advanced by fast-forward only;
- Coolify deployment `f6jm69yfz9qeh9r9gqllco9v` finished successfully;
- production is healthy at commit `dd055e251c455086ddcb02bc302d9f406b05d6ce`;
- catalog remains 62 tools with hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Current Step 70 candidate:
- closed an allowlist-bypass class where path-qualified executables were normalized to their basename;
- repository-local or arbitrary paths such as `./git`, `../git`, `/usr/bin/git`, Windows drive paths and whitespace-disguised names can no longer impersonate an allowlisted program;
- bare executable names such as `git`, `GIT`, `git.exe` and `go` remain compatible;
- always-blocked shells and privilege/network tools still retain the stronger destructive-command classification even when path-qualified.

Step 70 verification:
- RED failed because path-qualified allowlisted names were accepted and no dedicated error existed;
- focused command-policy tests passed after the fix;
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and `git diff --check` passed.

Next autonomous step: harden trusted executable resolution so a hostile or misconfigured PATH cannot redirect a bare allowlisted name to a repository-local executable. Do not publish, merge or deploy P4 without explicit owner approval.
