# Handoff: ephemeral human access grants

Date: 2026-06-29

Implemented L1 access grants for secret-path reads without touching L2/L3.
Also added the VPS/Coolify deploy path in a separate step.

What changed:

- `internal/policy`: added in-memory `AccessGrants` and `CheckReadWithAccess`.
  `CheckRead` still denies secrets normally.
- `internal/tools`: `read_file` returns structured `access-required`; approved
  normal grants still redact; raw output requires raw grant.
- `internal/grantadmin`: local loopback admin handler approves pending requests
  using a daemon-generated admin token.
- `cmd/mcp-devbox`: daemon starts loopback grant admin channel and prints local
  approval command; new `mcp-devbox grant` CLI approves grants. `--raw` requires
  `--confirm-raw`.
- `internal/mcpserver`: `read_file` accepts `access_request_id` and `raw`.
- `internal/mcpserver`: unauthenticated `GET /mcp` now returns `405` so deploy
  smoke checks match the documented streamable-HTTP behavior; `POST /mcp` without
  token still returns `401`.
- `Dockerfile`, `.dockerignore`, `docs/deploy-coolify.md`: Coolify/VPS deployment
  path for repos mounted at `/repos`, non-root runtime, HTTP on container port 8765,
  and token supplied via `MCP_DEVBOX_TOKEN`.

Security notes:

- No MCP tool can approve a grant.
- Grants are exact resolved path, single-use, TTL-bound, and in memory only.
- Raw secret output is separate from a normal grant.
- Requests and approval decisions are audited.
- Coolify deployment keeps policy enforcement inside the container; Traefik only
  fronts the HTTP transport.

Verification:

- `go test ./...` passed.
- `go vet ./...` passed.
- `go build ./...` passed.
- `gofmt -l` on touched Go files returned no output.
- `docker build -t mcp-devbox:coolify-smoke .` passed on Docker Desktop 4.55.0.
- Docker smoke container passed: `/healthz` -> 200, `GET /mcp` -> 405,
  `POST /mcp` without token -> 401, `POST /mcp?key=smoke-token` initialize -> 200,
  healthcheck `healthy`.
