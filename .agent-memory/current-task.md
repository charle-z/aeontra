# P3 composition root

Status: in progress on branch `p3-composition-root` from deployed `main` commit `ea332d173b4be1908bcf1c1abbe77ece610a6761`.

Completed:
- Step 63 `73f9df4`: extracted the command composition root into `internal/app` and reduced `cmd/mcp-devbox/main.go` to `app.Main()`.
- Step 64 `fd6d2ac`: split application orchestration into command, environment, OAuth, serve/bootstrap and grant modules.
- Step 65 `9bde22e`: isolated serve flag/env normalization in a tested immutable options parser.
- Step 66 `45ee0fa`: extracted policy/audit/service/server and optional integration composition into `appRuntime`.

Current Step 67 candidate:
- extracted loopback grant-admin startup, notifier wiring, token generation and bounded shutdown into `grant_admin.go`;
- extracted stdio/HTTP selection, bearer/OAuth resolution, fail-closed authentication, address normalization, signal draining and transport diagnostics into `transport.go`;
- reduced `serve.go` to parse options -> build runtime -> start local admin -> resolve transport -> serve;
- startup diagnostics continue to hide the admin token until a local access request requires the human approval command.

Step 67 verification:
- RED failed because transport resolution and local grant-admin lifecycle abstractions did not exist;
- tests passed for loopback address normalization, stdio posture, HTTP fail-closed behavior, bearer precedence, OAuth configuration and local grant-admin lifecycle/token secrecy;
- full tests, `go vet ./...`, and `go build ./...` passed.

Next autonomous step: add a package boundary guard and command-level compatibility tests, then close P3 documentation/baseline and run the final branch audit. Do not publish, merge or deploy P3 without explicit owner approval.
