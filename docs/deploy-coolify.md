# Deploy mcp-devbox on a VPS with Coolify

Goal: expose mcp-devbox at a stable HTTPS domain for ChatGPT web, while the daemon
works only on repositories cloned on the VPS under `/repos`. Do not expose a
personal machine.

## Image behavior

The Docker image is multi-stage:

- build: official `golang:1.26-alpine`, `go build ./cmd/mcp-devbox`
- runtime: `alpine`, non-root user `10001`, includes `git` and `wget`
- healthcheck: `GET http://127.0.0.1:8765/healthz`

Default container command:

```bash
mcp-devbox serve --root "${MCP_DEVBOX_ROOT:-/repos/workspace}" \
  --mode "${MCP_DEVBOX_MODE:-read-only}" \
  --http 0.0.0.0:8765
```

Set `MCP_DEVBOX_ROOT=/repos/<repo>` in Coolify for the repo ChatGPT should see.
The token must come from Coolify secrets/env as `MCP_DEVBOX_TOKEN`; never hardcode
it in the image or repo.

## Coolify setup

1. Create a new Coolify application from this repository, using the Dockerfile.
2. Set the public domain, for example `https://mcp.example.com`, and enable TLS.
3. Configure environment variables:

```text
MCP_DEVBOX_TOKEN=<long-random-secret>
MCP_DEVBOX_ROOT=/repos/<repo>
MCP_DEVBOX_MODE=read-only
```

Use `read-only` first. Switch to `ask` only when you intentionally want patch/test
workflows.

4. Add a persistent volume mounted at `/repos`.
5. Expose only the internal application port `8765` through Coolify/Traefik. Do not
publish `8765` directly on the VPS host firewall.
6. Deploy.

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

To update:

```bash
cd /repos/<repo>
git pull --ff-only
```

Make sure `MCP_DEVBOX_ROOT` points to the exact repo directory, not `/repos`, unless
you intentionally want the whole volume in the jail.

## Connect ChatGPT

ChatGPT's connector UI does not provide a custom bearer header field, so use the
query-string key and choose "Sin autenticacion":

```text
https://mcp.example.com/mcp?key=<MCP_DEVBOX_TOKEN>
```

Rotate `MCP_DEVBOX_TOKEN` immediately if it appears in logs, browser history, or a
shared screenshot.

## Recommended second gate

Do not rely only on `?key=` for a public VPS domain. Add one of these in front:

- Cloudflare Access for `mcp.example.com`
- Traefik `basicAuth` middleware
- Traefik `forwardAuth` to your identity provider

Keep the daemon token enabled even behind that gate; it is defense in depth.

## Verification against the real domain

After deployment, run from your local machine:

```bash
curl -i https://mcp.example.com/healthz
curl -i https://mcp.example.com/mcp
curl -i "https://mcp.example.com/mcp?key=<MCP_DEVBOX_TOKEN>"
curl -i -X POST "https://mcp.example.com/mcp" \
  -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
curl -i -X POST "https://mcp.example.com/mcp?key=<MCP_DEVBOX_TOKEN>" \
  -H "Content-Type: application/json" \
  --data '{"jsonrpc":"2.0","id":1,"method":"initialize"}'
```

Expected:

- `/healthz` returns `200`
- `GET /mcp` without token returns `401`
- `GET /mcp?key=<token>` returns `200` with `text/event-stream`
- `POST /mcp` without token returns `401`
- `POST /mcp?key=<token>` returns an MCP `initialize` result and an
  `Mcp-Session-Id` response header

Security invariants remain enforced by mcp-devbox policy inside the container:
jail, secret deny plus redaction, command allowlist, patch-first writes, and audit.
