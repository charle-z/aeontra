# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

P2 release verification:
- feature branch published and `main` advanced by fast-forward only;
- Coolify deployment `f3fqvldwq0totjrcvvhjq26c` finished successfully;
- production is healthy at commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`;
- catalog remains 62 tools with hash `sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

Current Step 63 candidate:
- added a RED AST contract requiring `cmd/mcp-devbox/main.go` to contain only one delegation to `app.Main()`;
- moved all existing process orchestration intact into `internal/app`;
- reduced the command entrypoint to one import and one delegation;
- moved the environment fallback and build-commit tests with the implementation;
- preserved all commands, flags, env names, output paths and process-exit behavior.

Step 63 verification:
- RED failed because the previous main imported 23 packages and owned the full application;
- focused command/app tests passed after extraction;
- `go run ./cmd/mcp-devbox version` preserved runtime identity output;
- `go test ./... -count=1`, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: split the 500-line `internal/app/app.go` into dispatch, environment, serve/bootstrap, and grant modules without behavior changes. Do not publish, merge or deploy P3 without explicit owner approval.
