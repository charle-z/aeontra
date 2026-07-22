# Deploy mcp-devbox on a VPS with Coolify

Goal: expose mcp-devbox at a stable HTTPS domain for ChatGPT web, while the daemon
works only on repositories cloned on the VPS under `/repos`. Do not expose a
personal machine.

## Image behavior

The Docker image is multi-stage:

- console build: installs pinned dependencies and runs only `console:build`; lint,
  typecheck and tests remain mandatory CI gates and are not repeated on the VPS
- Go build: official `golang:1.26-alpine`, persistent BuildKit module/compiler
  caches, and one-package/one-logical-CPU concurrency by default
- runtime: pinned Go/Alpine image, non-root user `10001`, includes Go, `git`,
  Node.js, and the BusyBox `wget` applet used only by the healthcheck
- healthcheck: `GET http://127.0.0.1:8765/healthz`
- graceful replacement: Docker `SIGTERM` is handled and active requests get up to five seconds to drain before exit.

The conservative build defaults are intentional for the production micro VPS with
two vCPUs. They leave roughly one logical CPU available to Coolify, Traefik and the
running application instead of letting compilation occupy both cores. Builds can
take longer, but normal requests should remain responsive. Dedicated build hosts can
override these Docker build arguments:

```text
BUILD_GOMAXPROCS=2
BUILD_GO_PARALLELISM=2
BUILD_UV_THREADPOOL_SIZE=2
```

Do not raise them on the two-vCPU production VPS merely to shorten a deployment.
The build uses cache mounts, so unchanged Go modules and compiled packages are reused
across BuildKit builds even though the final binary is stamped with the new commit.

Default container command:

```bash
mcp-devbox serve --root "${MCP_DEVBOX_ROOT:-/repos/workspace}" \
  --mode "${MCP_DEVBOX_MODE:-read-only}" \
  --http 0.0.0.0:8765
```

Set `MCP_DEVBOX_ROOT=/repos` in Coolify when using the global-builder workflow.
Repos live as child directories under that volume and tools accept `repo`/`cwd`
selectors for repo-local work.
The token must come from Coolify secrets/env as `MCP_DEVBOX_TOKEN`; never hardcode
it in the image or repo.

P9 Brain is optional at startup. To enable it, mount a dedicated persistent volume at
`/brain` and set:

```text
MCP_DEVBOX_BRAIN_ROOT=/brain
MCP_DEVBOX_STATE_ROOT=/state
MCP_DEVBOX_OBSERVABILITY=file
```

The Brain path must be absolute and disjoint from `/repos`. A configured but invalid
Brain fails startup; an unset Brain keeps the five tools registered but disabled.
The P9 candidate exposes 67 tools while the deployed P8 baseline remains at 62 until
the release PR, deployment, and smoke verification complete.

## Coolify setup

1. Create a new Coolify application from this repository, using the Dockerfile.
2. Set the public domain, for example `https://mcp.example.com`, and enable TLS.
3. Configure environment variables:

```text
MCP_DEVBOX_TOKEN=<long-random-secret>
MCP_DEVBOX_ROOT=/repos
MCP_DEVBOX_MODE=ask
MCP_DEVBOX_ALLOW_CMD=git,go,node,npm
MCP_DEVBOX_BRAIN_ROOT=/brain
```

Use `read-only` for inspection-only deployments. Use `ask` for the global-builder
workflow so patches, commands, commits, pushes, and deploys require explicit
approval fields.

If using OAuth for the ChatGPT connector, also set:

```text
MCP_DEVBOX_PUBLIC_URL=https://mcp.example.com
MCP_DEVBOX_OAUTH_PASSPHRASE=<long-owner-passphrase>
MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json
MCP_DEVBOX_OAUTH_REFRESH_STORE=/state/oauth-refresh.json
```

`MCP_DEVBOX_OAUTH_CLIENT_STORE` persists only ChatGPT's public OAuth client
registration. `MCP_DEVBOX_OAUTH_REFRESH_STORE` (0600) additionally persists **refresh
tokens** so a redeploy does not force the passphrase login again; access tokens and
authorization codes are never persisted. Mount both on `/state` (outside the MCP repo
jail) so agents cannot edit OAuth server state through normal file tools.

### Verify a redeploy actually shipped

`GET https://mcp.example.com/version` returns JSON containing only the live semantic
version, MCP protocol version, commit, optional build time, registered tool count,
and deterministic catalog hash. The same commit/hash/count are sent as response
headers. Compare these values before reconnecting a client: if they match the pushed
build while the client still shows an older tool surface, the remaining staleness is
client-side rather than a failed server deployment. Run the repository-native
smoke check from the expected source commit:

```bash
go run ./cmd/mcp-catalog-smoke \
  --url https://mcp.example.com \
  --expected-commit "$(git rev-parse HEAD)"
```

When Brain is enabled, run the additional read-only smoke. It prints only runtime
identity, index readiness/schema, note count, and context byte count:

```bash
go run ./cmd/brain-smoke \
  --url https://mcp.example.com \
  --expected-commit "$(git rev-parse HEAD)"
```

See `docs/runbooks/brain-operations.md` for permissions, backup, restore, update,
rollback, and troubleshooting.

See `docs/runbooks/catalog-cache.md` for diagnosis and rollback.

`GET https://mcp.example.com/healthz` returns `ok mcp-devbox <version> <commit>`. Compare
`<commit>` to `git rev-parse HEAD` on `main`. If it lags, the deploy did not ship the
latest code — check the webhook fired and that Coolify rebuilt (didn't reuse a cached
image). To stamp the commit, either let Coolify inject `SOURCE_COMMIT` (read at startup)
or pass a build argument `GIT_SHA=$(git rev-parse HEAD)` (baked into `internal/buildinfo` via `-ldflags`). Changing
`GIT_SHA` also busts the Docker build cache, forcing a genuine rebuild per commit.

4. Add persistent volumes:

```text
/repos
/state
/brain
```

Use `/repos` for cloned repositories and `/state` for OAuth client/refresh state,
bounded telemetry (`telemetry/metrics.db`) and four-segment operational logs under
`logs/`. Use the dedicated `/brain` volume for Markdown truth and local Git history. The
SQLite cache under `/brain/.cache` is disposable. The image prepares `/brain` for
UID/GID `10001:10001`; host bind mounts must preserve that ownership and private
`0700`/`0600` modes.
5. Optional global-builder env:

```text
GITHUB_TOKEN=<fine-grained-token>
GITHUB_OWNER=<owner>
GITHUB_OWNER_TYPE=user
GITHUB_DEFAULT_VISIBILITY=private

COOLIFY_URL=<your-coolify-url>
COOLIFY_API_TOKEN=<coolify-api-token>
COOLIFY_SERVER_UUID=<server-uuid>
COOLIFY_PROJECT_UUID=<project-uuid>
COOLIFY_ENVIRONMENT_NAME=production
COOLIFY_ALLOWED_DOMAINS=example.com
# Optional: use the configured Coolify GitHub App source for private repositories.
COOLIFY_GITHUB_APP_UUID=<coolify-github-app-uuid>

# Disabled by default; enable only fixed administrator-approved profiles:
MCP_DEVBOX_PRIVILEGED_TASKS=false
MCP_DEVBOX_PRIVILEGED_SERVICES=mcp-devbox
MCP_DEVBOX_PRIVILEGED_TIMEOUT=2m
```

`GITHUB_TOKEN` should have the minimum repo permissions you need. Do not enable
Coolify `read:sensitive`; mcp-devbox does not need secret-reading API responses for
builder actions.

The public MCP container must not mount `/var/run/docker.sock`. Docker privileged
profiles intentionally fail securely there. Run them only through a separately
contained administrator runner if that architecture is added later.
6. Expose only the internal application port `8765` through Coolify/Traefik. Do not
publish `8765` directly on the VPS host firewall.
7. Deploy.

### Rolling updates without dropping the old container

The repository is prepared for Coolify rolling replacement: Dockerfile deployment, no fixed `container_name`, no host port binding, an internal `/healthz` check with a startup window, and graceful `SIGTERM` handling. Keep `/repos`, `/state`, and `/brain` as persistent volumes. Coolify must keep the previous healthy container serving until the candidate passes its healthcheck, then switch Traefik traffic and stop the old instance. A failed candidate must not replace the healthy instance. During the brief overlap, avoid starting consequential writes from two clients at once; the overlap is for readiness and traffic handoff, not parallel agent execution.

If you manage Traefik labels manually, the service should route HTTPS traffic to
container port `8765` only. Example labels:

```text
traefik.enable=true
traefik.http.routers.mcp-devbox.rule=Host(`mcp.example.com`)
traefik.http.routers.mcp-devbox.entrypoints=https
traefik.http.routers.mcp-devbox.tls=true
traefik.http.services.mcp-devbox.loadbalancer.server.port=8765
```

## Clone and update repositories in `/repos`

Clone repos into the persistent `/repos` volume from the VPS/Coolify shell:

```bash
cd /repos
git clone https://github.com/OWNER/REPO.git <repo>
```

For private repos, use a deploy key or token available only on the VPS. Do not put
repo credentials in the mcp-devbox image or in ChatGPT prompts.

To update through MCP, use `repo_fetch`, `repo_fast_forward_preview`, then
`repo_fast_forward`. This preserves the jailed, audited, exact-plan workflow and
never uses reset. Reserve direct host Git commands for an administrator operating
outside the MCP protocol.

For global-builder mode, keep `MCP_DEVBOX_ROOT=/repos`. ChatGPT should use
`list_dir`, then pass `repo:"<repo>"` or `cwd:"<repo>"` to repo-local tools.

## Connect ChatGPT

Deploy with `MCP_DEVBOX_PUBLIC_URL` and `MCP_DEVBOX_OAUTH_PASSPHRASE`, then configure ChatGPT with:

```text
https://mcp.example.com/mcp
```

Choose OAuth. Persist `MCP_DEVBOX_OAUTH_CLIENT_STORE` and `MCP_DEVBOX_OAUTH_REFRESH_STORE` on `/state` so a container replacement can reconnect without recreating the connector. `MCP_DEVBOX_TOKEN` is optional recovery-only and must travel in an Authorization header, never a URL.

P8.1 rejects all `?key=` query credentials with HTTP 401.

## Recommended second gate

OAuth is necessary but can still be combined with a second public-edge gate. Add one of these in front:

- Cloudflare Access for `mcp.example.com`
- Traefik `basicAuth` middleware
- Traefik `forwardAuth` to your identity provider

Keep the daemon token enabled even behind that gate; it is defense in depth.

## Verification against the real domain

After deployment, run from your local machine:

```bash
curl -i https://mcp.example.com/healthz
curl -i https://mcp.example.com/version
curl -i https://mcp.example.com/mcp
curl -i -H "Authorization: Bearer <MCP_DEVBOX_TOKEN>" https://mcp.example.com/mcp
curl -i -X POST https://mcp.example.com/mcp \
  -H "Authorization: Bearer <MCP_DEVBOX_TOKEN>" \
  -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
```

Expected:

- `/healthz` returns the deployed commit
- `/version` returns the same commit and catalog headers
- unauthenticated `GET/POST /mcp` returns `401`
- OAuth or header bearer authorizes the MCP stream/request
- `/mcp?key=<even-correct-token>` returns `401`

Security invariants remain enforced by mcp-devbox policy inside the container:
jail, secret deny plus redaction, command allowlist, patch-first writes, and audit.

## Redeploy and reconnect

After an authorized push, let the existing Coolify webhook rebuild the branch.
Verify `/healthz` reports the pushed commit before testing tools. Keep `/repos`,
`/state`, and `/brain` volumes mounted. With persisted OAuth client and refresh stores, ChatGPT
should reconnect without connector deletion; if OAuth configuration changed,
reconnect once through the normal OAuth flow. Then call `tools/list`, confirm the documented tool count in `docs/tools.md` and all four annotations, and run read-only acceptance tests before any write.

> P8.1 security change: query-string credentials are rejected with HTTP 401. Configure the clean `/mcp` URL with OAuth. Header bearer remains recovery-only.
