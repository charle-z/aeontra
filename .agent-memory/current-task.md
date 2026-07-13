# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

Completed:
- Step 63 `73f9df4`: moved process orchestration into `internal/app`, reduced `cmd/mcp-devbox/main.go` to `app.Main()`, moved its tests, and added an AST composition-root guard.

Current Step 64 candidate:
- replaced the temporary 500-line `internal/app/app.go` with concern-based modules:
  - `run.go` for command dispatch and usage;
  - `env.go` for the frozen environment-variable contracts and parsing helpers;
  - `oauth.go` for OAuth construction;
  - `serve.go` for daemon bootstrap, service wiring, grant-admin lifecycle and transports;
  - `grant.go` for the local grant client;
- added a RED layout test that rejects the app monolith;
- preserved the version command and all existing behavior.

Step 64 verification:
- RED failed because none of the focused modules existed and `app.go` remained;
- focused app tests passed after the split;
- `go run ./cmd/mcp-devbox version` preserved runtime identity output;
- `go test ./... -count=1`, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: isolate serve flag/env normalization from runtime construction and add exact compatibility tests for flags, environment precedence, roots, allowlists, test commands, sandbox and audit path. Do not publish, merge or deploy P3 without explicit owner approval.
