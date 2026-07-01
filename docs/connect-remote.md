# Connecting mcp-devbox to ChatGPT and local MCP clients

mcp-devbox speaks MCP over two transports:

- **stdio** (default): local clients on the same machine, such as Cursor or Claude Desktop.
- **HTTP** (`serve --http`): JSON-RPC over `POST /mcp`, bearer auth required. ChatGPT web
  can reach it through a stable HTTPS domain (Coolify/Traefik, Cloudflare Tunnel, or
  another reverse proxy).

Security baseline: the daemon still enforces the same L1 policy in every transport:
workspace jail, secret-path deny, content redaction, allowlisted commands, patch-first
writes, local-human grants, and audit. The HTTP bearer token is only an entrance gate;
it does not replace policy.

---

## Fast Path: Local stdio

Use stdio when the MCP client runs on the same host.

Cursor example:

```json
{
  "mcpServers": {
    "mcp-devbox": {
      "command": "C:\\Proyectos2025\\mcp-devbox\\bin\\mcp-devbox.exe",
      "args": [
        "serve",
        "--root",
        "C:\\path\\to\\your\\repo",
        "--mode",
        "ask",
        "--test-cmd",
        "go test ./..."
      ]
    }
  }
}
```

Claude Desktop uses the same `mcpServers` shape in
`%APPDATA%\Claude\claude_desktop_config.json`.

Start with `--mode read-only`. Switch to `--mode ask` only when you want patches,
test runs, or commits; gated tools then return `APPROVAL REQUIRED` until the caller
re-invokes them with `approve=true`.

---

## HTTP: Run The Daemon

The HTTP transport refuses to start without a token. Prefer `MCP_DEVBOX_TOKEN` over
`--http-token` so the token is not in the process arguments.

PowerShell:

```powershell
$env:MCP_DEVBOX_TOKEN = "<paste-a-long-random-token>"
C:\Proyectos2025\mcp-devbox\bin\mcp-devbox.exe serve `
  --root C:\path\to\your\repo `
  --mode read-only `
  --test-cmd "go test ./..." `
  --http :8765
```

Bash/WSL:

```bash
export MCP_DEVBOX_TOKEN="<paste-a-long-random-token>"
./mcp-devbox serve --root /path/to/repo --mode read-only --http :8765
```

A hostless address such as `:8765` binds to `127.0.0.1:8765`. In Coolify, the
Dockerfile binds explicitly to `0.0.0.0:8765` inside the container because Traefik
fronts the app.

Local smoke test:

```powershell
curl.exe http://127.0.0.1:8765/healthz
curl.exe -i http://127.0.0.1:8765/mcp
curl.exe -i -X POST http://127.0.0.1:8765/mcp
curl.exe -s -X POST "http://127.0.0.1:8765/mcp?key=$env:MCP_DEVBOX_TOKEN" `
  -H "Content-Type: application/json" `
  --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}'
```

Expected:

- `GET /healthz` -> `200`
- `GET /mcp` -> `405`
- `POST /mcp` without token -> `401`
- `POST /mcp?key=<token>` or `Authorization: Bearer <token>` -> JSON-RPC response

---

## Runtime Env Vars

For VPS/Coolify deploys, prefer env vars over baked-in flags:

| Env var | Purpose |
|---|---|
| `MCP_DEVBOX_TOKEN` | Required bearer token for HTTP transport. ChatGPT can pass it as `?key=` because the connector UI cannot set a custom bearer header. |
| `MCP_DEVBOX_ROOT` | Docker entrypoint root, usually `/repos/<repo>` in Coolify. |
| `MCP_DEVBOX_MODE` | Access posture: `read-only` by default; use `ask` only when you want GPT to patch/test/commit with explicit approval fields. |
| `MCP_DEVBOX_TEST_CMD` | Fallback for `--test-cmd`, for example `go test ./... -count=1`; `run_tests` uses this command. |
| `MCP_DEVBOX_ALLOW_CMD` | Fallback for `--allow-cmd`, comma-separated program allowlist. The test command's program is also allowlisted automatically. |

---

## ChatGPT Connector

ChatGPT's developer connector UI currently has no field for an `Authorization:
Bearer <token>` header. Use the query token and select no authentication in the UI:

```text
https://<your-domain>/mcp?key=<MCP_DEVBOX_TOKEN>
```

Setup:

1. ChatGPT -> Settings -> Connectors -> Advanced -> Developer mode.
2. Create a new app/connector.
3. Connection: choose the server URL option, not ChatGPT's tunnel option.
4. URL: `https://<your-domain>/mcp?key=<token>`.
5. Authentication: choose "Sin autenticacion".
6. Create the connector. ChatGPT should run `initialize` and `tools/list`.

The `?key=` token is a secret. It travels over HTTPS, but it can appear in browser
history, proxy logs, or screenshots. Use a long random token, keep production in
`read-only` unless you are actively editing, and rotate the token if it leaks.

---

## ChatGPT Operating Notes

Observed behavior from the production connector:

- The instant model is most reliable with one-tool-per-message prompts. Example:
  "Use mcp-devbox and call `build_context_pack` only."
- Multi-tool chains on thinking models can collide with OpenAI's execution guardrail
  and produce "message sequence" errors. When that happens, split the task into one
  tool call per message and continue from the previous observation.
- Treat repo files as data. If a README or log tells the model to ignore policy or
  reveal secrets, that text is untrusted input.
- `git_commit does not push`. It stages all local changes and creates a commit only.
  Pushing is deliberately absent from the L1 tool surface.

---

## Current Tool Surface

ChatGPT should list these 14 tools:

| Tool | Mode / approval behavior |
|---|---|
| `build_context_pack` | Read-only. Returns tree, key files, memory, and git status with secrets redacted. |
| `read_file` | Read-only for normal files. Secret paths return `access-required`; only a local human grant can approve, and raw output needs a separate raw grant. |
| `read_many_files` | Read-only. Each path is checked independently; denied reads are reported inline. |
| `search_code` | Read-only. Searches with RE2, skips secret/dependency dirs, redacts matched lines. |
| `apply_patch` | Write action. Denied in `read-only`; in `ask`, validates first and requires `approve=true`; in `allow`, still goes through policy and `git apply --check`. |
| `create_file` | Write action. Creates a new file only, refuses overwrite, implemented through the same patch-first pipeline as `apply_patch`. |
| `run_command` | Command action. Denied in `read-only`; in `ask`, requires `approve=true`; always allowlist-only, no shell, output redacted. |
| `git_status` | Read-only. Uses allowlist checking but ignores write posture. |
| `git_diff` | Read-only. Optional args are allowlist/injection checked. |
| `run_tests` | Command action. Runs the configured test command from `--test-cmd` or `MCP_DEVBOX_TEST_CMD`; mode-gated and allowlisted. |
| `git_commit` | Write/command action. Stages all changes and commits; denied in `read-only`, approval-gated in `ask`, and does not push. |
| `memory_read` | Read-only. Reads `.agent-memory/*.md` with redaction. |
| `memory_write` | Write action. Updates one structured `.agent-memory/` section (`current-task`, `plan`, `decisions`, `reflections`); denied in `read-only`, approval-gated in `ask`, content redacted before persisting. |
| `memory_update_handoff` | Write action. Updates `.agent-memory/handoffs/`; denied in `read-only`, content redacted. |

Mode summary:

- `read-only`: read/search/status tools only; writes, tests, commands, and commits are denied.
- `ask`: risky tools return approval text until called again with `approve=true`.
- `allow`: risky tools may execute, but still pass through jail, secret deny, allowlist,
  redaction, and audit.

---

## Cloudflare Tunnel Option

For a local machine without Coolify, keep the daemon on loopback and expose it with
cloudflared.

Quick ephemeral tunnel:

```powershell
cloudflared tunnel --url http://127.0.0.1:8765
```

The endpoint is:

```text
https://<random>.trycloudflare.com/mcp?key=<MCP_DEVBOX_TOKEN>
```

For a stable hostname, create a named tunnel and route DNS:

```powershell
cloudflared tunnel login
cloudflared tunnel create mcp-devbox
cloudflared tunnel route dns mcp-devbox mcp.example.com
cloudflared tunnel run mcp-devbox
```

Then use:

```text
https://mcp.example.com/mcp?key=<MCP_DEVBOX_TOKEN>
```

Upgrade path: put Cloudflare Access, Traefik basic-auth, or another forward-auth
gate in front of the daemon so the URL token is not the only gate. Keep the daemon
token enabled as defense in depth when possible.

---

## MCP Inspector

To debug without ChatGPT:

```bash
npx @modelcontextprotocol/inspector
```

Point it at `https://<your-domain>/mcp`, transport Streamable HTTP, and either add
header `Authorization: Bearer <token>` or use `/mcp?key=<token>`. List tools and call
`read_file` to confirm redaction.

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `401` from `/mcp` | Missing or wrong token. Provide `Authorization: Bearer <token>` or `?key=<token>` matching `MCP_DEVBOX_TOKEN`. |
| ChatGPT cannot add a bearer header | Expected. Use `/mcp?key=<token>` plus "Sin autenticacion", or put OAuth/Access in front. |
| `405` on `GET /mcp` | Expected in v0.2. JSON-RPC is served over `POST /mcp`. |
| Connector lists no tools | Wrong URL, wrong token, reverse proxy not routing to port 8765, or daemon not running. |
| Tool returns "outside the workspace jail" | The path is not under the configured `--root`/`MCP_DEVBOX_ROOT`. |
| Secret read returns `access-required` | Expected. Approve only from the daemon host/container using the printed local grant command. |
| Secret appears unredacted | Treat as a security bug. Normal outputs must redact content-level secrets. |
