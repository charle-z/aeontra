# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

Completed:
- Step 63 `73f9df4`: extracted the command composition root into `internal/app` and reduced `cmd/mcp-devbox/main.go` to `app.Main()`.
- Step 64 `fd6d2ac`: split application orchestration into command, environment, OAuth, serve/bootstrap and grant modules.

Current Step 65 candidate:
- added `serveOptions` and `parseServeOptions` so CLI/env normalization is independent of daemon construction;
- preserved repeatable/comma-separated roots, mode, audit path, HTTP address/token, command allowlist, test command and sandbox backend;
- flags continue to override `MCP_DEVBOX_ALLOW_CMD`, `MCP_DEVBOX_TEST_CMD`, and `MCP_DEVBOX_SANDBOX`;
- test-command programs are still appended uniquely to the effective allowlist;
- `serve` now consumes the validated immutable config rather than owning flag parsing.

Step 65 verification:
- RED failed because the parser did not exist;
- compatibility tests passed for flag precedence, existing env names, root handling, defaults and required-root failure;
- full tests, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: extract runtime/service composition from transport lifecycle and test optional GitHub, Coolify, validation, sandbox and privileged configuration without exposing secret values. Do not publish, merge or deploy P3 without explicit owner approval.
