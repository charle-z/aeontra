# Connecting mcp-devbox to ChatGPT and local MCP clients

mcp-devbox speaks MCP over two transports:

- **stdio** (default): local clients on the same machine, such as Cursor or Claude Desktop.
- **HTTP** (`serve --http`): JSON-RPC over `POST /mcp`, auth required. ChatGPT web
  can reach it through a stable HTTPS domain (Coolify/Traefik, Cloudflare Tunnel, or
  another reverse proxy). Auth is either a **static bearer token** (`?key=` fallback for
  ChatGPT) or **OAuth 2.1** — the recommended, secret-not-in-URL option. See
  [oauth.md](oauth.md).

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
curl.exe -i "http://127.0.0.1:8765/mcp?key=$env:MCP_DEVBOX_TOKEN"
curl.exe -i -X POST http://127.0.0.1:8765/mcp
curl.exe -s -X POST "http://127.0.0.1:8765/mcp?key=$env:MCP_DEVBOX_TOKEN" `
  -H "Content-Type: application/json" `
  --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}'
```

Expected:

- `GET /healthz` -> `200`
- `GET /mcp` without token -> `401`
- `GET /mcp?key=<token>` -> `200` with `Content-Type: text/event-stream`
- `POST /mcp` without token -> `401`
- `POST /mcp?key=<token>` or `Authorization: Bearer <token>` -> JSON-RPC response;
  `initialize` includes an `Mcp-Session-Id` response header

---

## Runtime Env Vars

For VPS/Coolify deploys, prefer env vars over baked-in flags:

| Env var | Purpose |
|---|---|
| `MCP_DEVBOX_TOKEN` | Required bearer token for HTTP transport. ChatGPT can pass it as `?key=` because the connector UI cannot set a custom bearer header. |
| `MCP_DEVBOX_ROOT` | Docker entrypoint root. Use `/repos` for global-builder mode. |
| `MCP_DEVBOX_MODE` | Access posture: `read-only` by default; use `ask` when you want GPT to patch/test/commit/push/deploy with explicit approval fields. |
| `MCP_DEVBOX_TEST_CMD` | Fallback for `--test-cmd`, for example `go test ./... -count=1`; `run_tests` uses this command. |
| `MCP_DEVBOX_ALLOW_CMD` | Fallback for `--allow-cmd`, comma-separated program allowlist. For global-builder web work, use `git,go,node,npm`. |
| `GITHUB_TOKEN` / `GITHUB_OWNER` / `GITHUB_OWNER_TYPE` | Optional GitHub tools config. `GITHUB_OWNER_TYPE` is `user` or `org`. |
| `GITHUB_DEFAULT_VISIBILITY` | Optional repo creation default, `private` unless set to `public`. |
| `COOLIFY_URL` / `COOLIFY_API_TOKEN` | Optional Coolify tools config. Do not grant `read:sensitive` unless you deliberately need secret-reading API responses outside mcp-devbox. |
| `COOLIFY_SERVER_UUID` / `COOLIFY_PROJECT_UUID` | Required for `coolify_create_app`. |
| `COOLIFY_ENVIRONMENT_NAME` / `COOLIFY_ENVIRONMENT_UUID` | One is required for `coolify_create_app`. |
| `COOLIFY_ALLOWED_DOMAINS` | Optional comma-separated domain suffix allowlist for `coolify_create_app`. |
| `MCP_DEVBOX_PRIVILEGED_TASKS` | `true` explicitly enables fixed privileged profiles; disabled by default. |
| `MCP_DEVBOX_PRIVILEGED_SERVICES` | Optional comma-separated service allowlist for status/restart profiles. |
| `MCP_DEVBOX_PRIVILEGED_TIMEOUT` | Fixed profile timeout, default `2m`. |

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
- Global-builder publishing is explicit. `git_commit does not push`; use
  `repo_publish_preview` then `repo_publish` only when requested. Use the planned
  `platform_app_create_*` and `platform_deploy_*` workflows for Coolify writes.
- Compatibility aliases such as `git_push`, `github_create_repo`, and old Coolify
  names invoke the same planned handlers; they are not a confirmation bypass.

---

## Current Tool Surface

The canonical 53-tool table, all four annotations, aliases and exact effects are in
[tools.md](tools.md). Recommended complete workflows are:

```text
repo_list
repo_status
repo_fetch
repo_fast_forward_preview
repo_fast_forward
source_repo_create_preview
source_repo_create
repo_remote_preview
repo_remote_set
repo_publish_preview
repo_publish
```

```text
platform_apps_list
platform_app_create_preview
platform_app_create
platform_deploy_preview
platform_deploy
platform_app_status
```

Use `notes_list`/`notes_read` and `notes_write_preview`/`notes_write` for free-form
notes. Use `privileged_task_preview`/`privileged_task_execute` only when the
administrator explicitly enabled fixed profiles. There is no free host terminal,
no force push, and tokens are never returned. External writes require explicit
approval in ask mode.

ChatGPT should list these tools:

| Tool | Mode / approval behavior |
|---|---|
| `build_context_pack` | Read-only. Optional `repo`; returns tree, key files, memory, and git status with secrets redacted. |
| `list_dir` | Read-only. Lists one jailed directory without reading file contents; useful for seeing repos under `/repos`; Git repos are marked `[git]`. |
| `read_file` | Read-only for normal files. Secret paths return `access-required`; only a local human grant can approve, and raw output needs a separate raw grant. |
| `read_many_files` | Read-only. Each path is checked independently; denied reads are reported inline. |
| `search_code` | Read-only. Searches with RE2, skips secret/dependency dirs, redacts matched lines. |
| `apply_patch` | Write action. Optional `repo`; denied in `read-only`; in `ask`, validates first and requires `approve=true`; in `allow`, still goes through policy and `git apply --check`. |
| `create_file` | Write action. Optional `repo`; creates a new file only, refuses overwrite, implemented through the same patch-first pipeline as `apply_patch`. |
| `run_command` | Command action. Optional `cwd` selects a jailed working directory such as `mcp-devbox`; denied in `read-only`; in `ask`, requires `approve=true`; always allowlist-only, no shell, output redacted. |
| `git_status` | Read-only. Optional `repo` selects a jailed repo directory when root is `/repos`; uses allowlist checking but ignores write posture. |
| `git_diff` | Read-only. Optional `repo`; optional args are allowlist/injection checked. |
| `git_clone` | Command/write action. Clones into a new simple directory under the root; rejects embedded credentials and target escapes; approval-gated in `ask`. |
| `git_push` | Compatibility alias for planned `repo_publish`; requires a preview plan and never accepts force/tags/refspecs/URL remotes. |
| `github_create_repo` | Compatibility alias for planned `source_repo_create`; private-by-default preview and owner revalidation happen first. |
| `github_repo_info` | Read-only. Reads basic metadata for a GitHub repo under the configured owner. |
| `run_tests` | Command action. Optional `cwd`; runs the configured test command from `--test-cmd` or `MCP_DEVBOX_TEST_CMD`; mode-gated and allowlisted. |
| `git_commit` | Write/command action. Optional `repo`; stages all changes and commits; denied in `read-only`, approval-gated in `ask`, and does not push. |
| `memory_read` | Read-only. Optional `repo`; reads `.agent-memory/*.md` with redaction. |
| `memory_write` | Write action. Optional `repo`; updates one structured `.agent-memory/` section (`current-task`, `plan`, `decisions`, `reflections`); denied in `read-only`, approval-gated in `ask`, content redacted before persisting. |
| `memory_update_handoff` | Write action. Updates `.agent-memory/handoffs/`; denied in `read-only`, content redacted. |
| `sandbox_status` | Read-only diagnostic. Reports whether an L3 sandbox backend is configured; default is unavailable, no free terminal, no Docker socket in the public MCP container. |
| `sandbox_exec` | Broad command execution inside an L3 sandbox only. Disabled unless a real sandbox backend is configured; not a replacement for L1 allowlist. |
| `coolify_deploy` | Compatibility alias for planned `platform_deploy`; app state is revalidated before the external write. |
| `coolify_list_apps` | Read-only. Lists Coolify apps when Coolify env is configured. |
| `coolify_app_status` | Read-only. Reads one Coolify app by uuid; `COOLIFY_ALLOWED_APPS` enforced when set. |
| `coolify_create_app` | Compatibility alias for planned `platform_app_create`; owner/domain/build/port/healthcheck are validated in preview. |
| `coolify_set_env` | External write action. Sets app env vars; values are redacted from output/audit; approval-gated in `ask`. |

When `MCP_DEVBOX_ROOT=/repos`, use this flow:

1. `list_dir` with no path to see available repos.
2. For an existing repo, call `build_context_pack` and `git_status` with
   `repo:"mcp-devbox"` or another listed repo.
3. For a new repo, use `git_clone` or `create_file` with a new repo directory, then
   initialize/commit/publish only when requested.
4. Use `apply_patch`, `create_file`, `git_commit`, and `memory_*` with `repo`.
5. Use `run_command` / `run_tests` with `cwd:"mcp-devbox"` for repo-local commands.

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
| `GET /mcp` returns `401` | Expected without auth. Add `?key=<token>` or a bearer header. |
| `GET /mcp?key=` returns `200 text/event-stream` | Expected after P1-6. It is a minimal SSE stream for MCP client compatibility; JSON-RPC calls still use `POST /mcp`. |
| Connector lists no tools | Wrong URL, wrong token, reverse proxy not routing to port 8765, or daemon not running. |
| Tool returns "outside the workspace jail" | The path is not under the configured `--root`/`MCP_DEVBOX_ROOT`. |
| Secret read returns `access-required` | Expected. Approve only from the daemon host/container using the printed local grant command. |
| Secret appears unredacted | Treat as a security bug. Normal outputs must redact content-level secrets. |
