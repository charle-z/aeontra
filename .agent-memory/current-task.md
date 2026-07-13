# P8 authenticated dark console

Status: active on branch `p8-authenticated-dark-console`, based on deployed P7
closure commit `30ae8a7e9d7b73584b34ef3bbbc952407faa5117`.

## Implemented locally

- Independent spec, plan, tasks, threat model, and operator guide.
- Dependency-free embedded dark HTML/CSS/JS in the existing Go HTTP application.
- No new Coolify application, listener, credential, npm dependency, CDN, or OAuth
  protocol change.
- Routes `/console`, login/logout, safe status, and authenticated embedded assets.
- Existing static token form login and existing OAuth/bearer direct bootstrap.
- Opaque 32-byte session ids; server stores only SHA-256 digests.
- Eight-hour TTL, cap 128, expiry/revocation/collision handling, in-memory only.
- HttpOnly, SameSite=Strict, path-scoped cookies; Secure for HTTPS/non-loopback.
- Exact status schema with only public runtime identity and presentation flags.
- CSP and browser hardening on pages, JSON, assets, errors, and redirects.
- No tool execution, approvals, repository/path/source/prompt/target/log/audit access.
- `cmd/console-smoke` validates authenticated production without printing token or cookie.
- Public MCP remains 62 tools with the unchanged catalog hash.

## Final local verification

Passed on the final implementation tree:

- `go fmt ./...`.
- `go test ./... -count=1`.
- atomic coverage plus package gate.
- `internal/console` 84.2% against an 80% minimum.
- `cmd/console-smoke` 76.5%.
- `go vet ./...`.
- `go build ./...`.
- actionlint v1.7.12.
- Govulncheck v1.6.0: no vulnerabilities.
- focused console/smoke/MCP/app/observability/workflow/Grype tests.
- documentation consistency and `git diff --check`.

Runner-authoritative gates:

- local Staticcheck cannot initialize `/home/mcpdevbox/.cache/staticcheck` because
  the non-root container has no writable home; GitHub uses `runner.temp`.
- local Race cannot start because CGO is disabled; GitHub runs the CGO-enabled job.
- CodeQL, Dependency Review, Docker build, SBOM, and Grype remain mandatory remotely.

## Next exact actions

1. Remove the final memory editor and audit/stage the exact P8 diff.
2. Commit/publish `p8-authenticated-dark-console` and fast-forward `main` through the authorized flow.
3. Observe Race, Staticcheck, CodeQL, SBOM, Grype, and all other Actions; fix real failures.
4. Verify exact production commit, health, 62 tools/hash, and run authenticated `console-smoke`.
5. Inspect safe `route=console` JSONL events, create the P8 baseline, and close before Asset Broker.

No public MCP tool/schema/annotation/approval or OAuth protocol change.
