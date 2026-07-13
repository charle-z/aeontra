# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

Completed:
- Step 63 `73f9df4`: extracted the command composition root into `internal/app` and reduced `cmd/mcp-devbox/main.go` to `app.Main()`.
- Step 64 `fd6d2ac`: split application orchestration into command, environment, OAuth, serve/bootstrap and grant modules.
- Step 65 `9bde22e`: isolated serve flag/env normalization in a tested immutable options parser.
- Step 66 `45ee0fa`: extracted policy/audit/service/server and optional integration composition into `appRuntime`.
- Step 67 `1966645`: isolated local grant-admin and stdio/HTTP transport lifecycle.

Current Step 68 candidate:
- changed `Main` into a one-line process adapter over a testable `run` command dispatcher;
- preserved version/help/serve/grant/unknown-command output and exit codes;
- `usage` now writes to an injected writer while production still uses stderr;
- added command-level compatibility tests for success, usage and error paths;
- added an AST boundary test requiring all MCP_DEVBOX, COOLIFY, GITHUB and SOURCE_COMMIT environment literals to remain centralized in `env.go`.

Step 68 verification:
- RED failed because the testable dispatcher did not exist;
- focused command and environment-boundary tests passed after refactoring;
- `go run ./cmd/mcp-devbox version` preserved the deployed identity format;
- full tests, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: P3 closure documentation and baseline, branch-vs-main audit, final quality gates and merge-readiness verdict. Do not publish, merge or deploy P3 without explicit owner approval.
