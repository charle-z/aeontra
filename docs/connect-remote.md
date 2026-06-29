# Connecting mcp-devbox to ChatGPT (and other clients)

mcp-devbox speaks MCP over two transports:

- **stdio** (default) — for local clients on the same machine (Cursor, Claude Desktop).
- **HTTP** (`serve --http`) — JSON-RPC over `POST /mcp`, **bearer auth required**, meant
  to be exposed to a cloud chat (ChatGPT) through a **self-hosted Cloudflare Tunnel**.

> Security note: the HTTP listener binds to `127.0.0.1` by default. It is **never**
> directly exposed to the LAN/Internet — a Cloudflare Tunnel connects *outbound* from
> your machine, so there are no inbound ports to open. The daemon still enforces its
> own bearer token (defense in depth) and all L1 invariants (jail, secret deny + redaction,
> allowlist, audit). See `SECURITY.md` for the honest scope.

---

## Fast path (no tunnel): dogfood today over stdio

The quickest way to use mcp-devbox right now — works offline, no auth/tunnel needed.

### Cursor
Add to `~/.cursor/mcp.json` (or the project `.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "mcp-devbox": {
      "command": "C:\\Proyectos2025\\mcp-devbox\\bin\\mcp-devbox.exe",
      "args": ["serve", "--root", "C:\\path\\to\\your\\repo", "--mode", "ask",
               "--test-cmd", "go test ./..."]
    }
  }
}
```

### Claude Desktop
Edit `%APPDATA%\Claude\claude_desktop_config.json` with the same `mcpServers` block,
then restart Claude Desktop. The tools appear under the 🔌 menu.

> Use `--mode read-only` to start; switch to `ask` when you want patches/tests
> (risky actions then return "APPROVAL REQUIRED" until you pass `approve=true`).

---

## Remote path: ChatGPT web via Cloudflare Tunnel

### Step 0 — Generate a strong bearer token

PowerShell:
```powershell
$b = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($b)
[Convert]::ToBase64String($b)   # copy this value
```
WSL2 / bash: `openssl rand -base64 32`

### Step 1 — Run the daemon with the HTTP transport

The token is read from the `MCP_DEVBOX_TOKEN` env var (preferred — keeps it out of the
process list). The daemon refuses to start over HTTP without a token.

PowerShell (Windows):
```powershell
$env:MCP_DEVBOX_TOKEN = "<paste-the-token>"
C:\Proyectos2025\mcp-devbox\bin\mcp-devbox.exe serve `
  --root C:\path\to\your\repo --mode ask --test-cmd "go test ./..." --http :8765
```
WSL2 (if you run the daemon inside Linux):
```bash
export MCP_DEVBOX_TOKEN="<paste-the-token>"
./mcp-devbox serve --root /mnt/c/path/to/repo --mode ask --http :8765
```

Sanity check locally (new terminal):
```powershell
curl.exe http://127.0.0.1:8765/healthz                       # -> ok
curl.exe -s -X POST http://127.0.0.1:8765/mcp `
  -H "Authorization: Bearer $env:MCP_DEVBOX_TOKEN" `
  -H "Content-Type: application/json" `
  --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}'
```
No token → `401`. With token → an `initialize` result.

### Step 2 — Install cloudflared

- **Windows:** `choco install cloudflared`  (or `winget install Cloudflare.cloudflared`,
  or download `cloudflared-windows-amd64.exe` from the Cloudflare GitHub releases).
- **WSL2 (Debian/Ubuntu):**
  ```bash
  curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 \
       -o /usr/local/bin/cloudflared && chmod +x /usr/local/bin/cloudflared
  ```

### Step 3a — Quick tunnel (ephemeral URL, zero config) — best for first test

```powershell
cloudflared tunnel --url http://127.0.0.1:8765
```
cloudflared prints a URL like `https://random-words.trycloudflare.com`. That is your
public base; the MCP endpoint is `https://random-words.trycloudflare.com/mcp`.
The URL changes every run (fine for testing, not for a permanent connector).

### Step 3b — Named tunnel (stable HTTPS URL) — for ongoing use

Requires a free Cloudflare account and a domain on Cloudflare.
```powershell
cloudflared tunnel login
cloudflared tunnel create mcp-devbox
cloudflared tunnel route dns mcp-devbox mcp.example.com
```
Create `config.yml` (path shown by `cloudflared tunnel info`; usually
`%USERPROFILE%\.cloudflared\config.yml`):
```yaml
tunnel: mcp-devbox
credentials-file: C:\Users\<you>\.cloudflared\<TUNNEL-UUID>.json
ingress:
  - hostname: mcp.example.com
    service: http://127.0.0.1:8765
  - service: http_status:404
```
Run it: `cloudflared tunnel run mcp-devbox`. Endpoint: `https://mcp.example.com/mcp`.

### Step 5 — Add the connector in ChatGPT (developer mode)

> Reality of the ChatGPT UI: the "Nueva aplicación" dialog has **Conexión**
> (URL del servidor / Túnel) and **Autenticación** = *OAuth / Sin autenticación /
> Mixta*. There is **no field for an `Authorization: Bearer` header.** So we carry the
> token in the URL as `?key=<token>` and pick **Sin autenticación**. (The daemon still
> authenticates every request — it just reads the token from the query string.)

1. ChatGPT → **Configuración → Conectores → Avanzado → Modo desarrollador** (enable it),
   then **Crear / Nueva aplicación**.
2. **Conexión:** keep **"URL del servidor"** (not "Túnel" — that toggle is for
   ChatGPT's own tunnel feature, unrelated to cloudflared).
3. **URL:** paste your endpoint **with the key in the query string**:
   ```
   https://<your-host>/mcp?key=<your-token>
   ```
   (`<your-host>` = the trycloudflare URL from Step 3a or your named-tunnel hostname;
   `<your-token>` = the value of `MCP_DEVBOX_TOKEN`.)
4. **Autenticación:** choose **"Sin autenticación"** (the key in the URL is the secret).
5. Tick **"Entiendo y quiero continuar"** and create.
6. ChatGPT runs `initialize` + `tools/list`; you should see the 10 tools
   (`build_context_pack`, `read_file`, …). If it errors, re-run the Step 1 curl through
   the **public** URL with `?key=` and confirm cloudflared is still running.

> Security tradeoff of `?key=`: the secret travels in the URL and may appear in proxy
> logs / history. For personal use over HTTPS this is acceptable — but use
> **`--mode read-only`**, a long random token, and rotate it if leaked. To remove the
> URL-secret entirely, use the **OAuth via Cloudflare Access** upgrade below.

### Step 4/Upgrade — OAuth via Cloudflare Access (most secure; needs a domain)

When you have a domain on Cloudflare (named tunnel, Step 3b), you can let ChatGPT use
its **OAuth** option instead of a URL secret:

1. Zero Trust → **Access → Applications** → add a **self-hosted** app for
   `mcp.example.com`, and enable Access's MCP/OAuth support so ChatGPT can complete an
   interactive login (your email) once.
2. In ChatGPT, set **Autenticación = OAuth** and URL `https://mcp.example.com/mcp`
   (no `?key=` needed — Cloudflare is the gate).
3. Keep the daemon bound to `127.0.0.1`; only cloudflared reaches it. You may keep the
   daemon token too (defense in depth) since cloudflared connects locally.

This removes the secret-in-URL weakness; the auth gate is Cloudflare's OAuth login.

### Step 6 — Verify a real tool call from ChatGPT

Ask ChatGPT something that forces a tool call, e.g.:

> "Use mcp-devbox: call build_context_pack and summarize the repo, then read README.md."

Expected: ChatGPT invokes the tool and returns content **with any secrets redacted**
(`***REDACTED-SECRET***`), confined to the configured `--root`. Confirm the call also
landed in the audit log: `<root>/.agent-memory/audit.log`.

---

## Independent verification with MCP Inspector

To debug the endpoint without ChatGPT:
```bash
npx @modelcontextprotocol/inspector
```
Point it at `https://<your-host>/mcp`, transport **Streamable HTTP**, and either add
header `Authorization: Bearer <token>` or use the URL `…/mcp?key=<token>`. List tools
and call `read_file` to confirm redaction.

---

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `401` from `/mcp` | Missing/wrong token. Provide it as `Authorization: Bearer <t>` **or** `?key=<t>`; must match `MCP_DEVBOX_TOKEN`. |
| ChatGPT can't add a header | Expected — use `…/mcp?key=<token>` + "Sin autenticación" (Step 5), or OAuth via Cloudflare Access. |
| `405` on `GET /mcp` | Expected — we serve JSON-RPC over **POST**; there is no server-initiated SSE in L1. |
| Connector lists no tools | cloudflared not running, wrong URL (must end in `/mcp`), or auth failing. Re-run the Step 1 curl through the public URL. |
| Tool returns "outside the workspace jail" | Path not under `--root`. By design. |
| Secret appears unredacted | File it as a security bug — redaction should cover every returned payload. |
