# P7 structured observability

Status: implementation candidate is complete on branch `p7-structured-observability`, based on deployed P6 closure commit `ab0cf153fe898784dac6d48a062de78abb4d5f5d`.

## Implemented

- Independent spec, plan, tasks, threat model, operator guide, and documentation regression test.
- Closed-schema JSONL sink under `internal/observability`.
- Modes `off|stderr|file|both`; default stderr; optional private bounded file rotation.
- Immutable flags/env: `MCP_DEVBOX_OBSERVABILITY`, `MCP_DEVBOX_OBSERVABILITY_PATH`, `MCP_DEVBOX_OBSERVABILITY_MAX_BYTES`.
- Dedicated default file path: `<root>/.agent-memory/observability/observability.jsonl`.
- Private directory/file posture: 0700/0600, one `.1` backup, bounded size, symlink/ancestor rejection, broad-directory rejection, and generic path-safe errors.
- Internally generated request IDs shared by HTTP and JSON-RPC events; client request IDs are ignored.
- Safe lifecycle, HTTP, JSON-RPC, batch parse, and known public tool completion events.
- Closed labels only: normalized route/method/tool/outcome/status/duration/error class and public build identity/count/hash.
- No prompts, bodies, params, results, source, paths, repository names, commands, targets, URLs, queries, headers, tokens, identities, IPs/domains, raw errors, or arbitrary attribute maps.
- Timestamp and schema version are always server-owned.
- Multi-writer mode continues writing to healthy destinations when another fails; failures are counted without retaining raw error text.
- Startup diagnostics no longer print roots, audit paths, bind address, or authentication details; off mode retains a sanitized diagnostic.
- Public catalog remains 62 tools with the unchanged expected hash.

## Final local verification

Passed:

- `go fmt ./...`.
- `go test ./... -count=1`.
- atomic coverage profile and package gate.
- `internal/observability` coverage 74.4% against a 70% minimum.
- `go vet ./...`.
- `go build ./...`.
- actionlint v1.7.12.
- govulncheck v1.6.0: no vulnerabilities.
- focused observability/app/mcpserver/workflow/Grype tests.
- `git diff --check`.

Runner-authoritative gates:

- Local Staticcheck cannot initialize `/home/mcpdevbox/.cache/staticcheck` because the deployed non-root container has no writable home. GitHub Actions uses `runner.temp` and must pass before merge/deploy.
- Local race cannot run because CGO is disabled. GitHub Actions must run the CGO-enabled race job.
- Docker build, SBOM, Grype, CodeQL, and Dependency Review remain mandatory in Actions.

## Next exact actions

1. Audit/stage the exact P7 diff and commit it.
2. Publish `p7-structured-observability`.
3. Because the current token may lack pull-request write permission, either create a PR through the connected GitHub action if available or fast-forward `main` only under the repository's authorized flow and observe all push gates.
4. Correct any reproducible CI/security failure before deployment.
5. Deploy only existing application `jqf7qz5ensoqtvl1tb197gcv`, preserve the deployment id if returned, and verify exact commit/health/62 tools/hash.
6. Inspect safe JSONL application logs and create the P7 closure baseline before starting the console milestone.

No public MCP tool/schema/annotation/approval/OAuth authority change.
