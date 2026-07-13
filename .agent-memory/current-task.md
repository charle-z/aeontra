# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

Completed:
- Step 63 `73f9df4`: extracted the command composition root into `internal/app` and reduced `cmd/mcp-devbox/main.go` to `app.Main()`.
- Step 64 `fd6d2ac`: split application orchestration into command, environment, OAuth, serve/bootstrap and grant modules.
- Step 65 `9bde22e`: isolated serve flag/env normalization in a tested immutable options parser.

Current Step 66 candidate:
- introduced `appRuntime` to compose policy, audit logger, capability service and MCP server independently of transport lifecycle;
- extracted sandbox, private validation, privileged-profile, Coolify and GitHub dependency builders;
- froze `MCP_DEVBOX_SANDBOX_IMAGE` alongside the existing environment contracts;
- optional integrations remain disabled unless their original configuration variables are present;
- runtime construction closes the audit logger on partial failure and exposes one explicit Close path.

Step 66 verification:
- RED failed because runtime builders and environment adapters did not exist;
- tests passed for privileged timeout/services, optional GitHub/Coolify/validation construction, sandbox posture and complete runtime composition;
- full tests, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: isolate local grant-admin and HTTP/stdio transport lifecycle, then add fail-closed auth and address-normalization tests without changing auth semantics. Do not publish, merge or deploy P3 without explicit owner approval.
