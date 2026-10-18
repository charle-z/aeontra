# coolify_deploy — trigger Coolify deploys from the chat

Lets an MCP client (ChatGPT, etc.) trigger a deploy of an existing Coolify
application by uuid — a controlled, gated capability, **disabled unless configured.**

## Enable it (env on the daemon / Coolify)
```
COOLIFY_URL=https://<your-coolify-host>          # base URL of your Coolify instance
COOLIFY_API_TOKEN=<coolify API token>            # SECRET — Coolify → Keys & Tokens
MCP_DEVBOX_ALLOWED_APPS is NOT this; use:
COOLIFY_ALLOWED_APPS=uuid1,uuid2                 # optional allowlist of app uuids (recommended)
```
Also keep `MCP_DEVBOX_MODE=ask` so deploys require your approval.

## Use it (from the chat)
Ask the agent to call `coolify_deploy` with the app `uuid` (find it in the app's
Coolify URL / settings). In `ask` mode it returns "APPROVAL REQUIRED" first; re-invoke
with `approve=true`.

## Security model
- The **API token is a secret**: read from env, sent ONLY in the `Authorization`
  header — never placed in the URL, returned to the agent, or logged. The response
  body is redacted before return.
- **No SSRF**: the base URL is fixed by `COOLIFY_URL`; the agent may only pass an
  `app` uuid, validated as `[a-zA-Z0-9]{1,64}` (no scheme/slash/query), so it cannot
  retarget the request.
- **Gated**: denied in `read-only`; requires `approve=true` in `ask`; optional
  `COOLIFY_ALLOWED_APPS` restricts which apps can be deployed. Every call is audited.
- **Disabled by default**: without `COOLIFY_URL`+`COOLIFY_API_TOKEN`, the tool
  returns "not configured".

## Notes
- Uses Coolify's deploy-by-uuid endpoint (`GET /api/v1/deploy?uuid=...&force=false`).
- This is a deliberate network capability of the daemon (not a sandboxed command);
  scope it with the allowlist and keep it in `ask` mode.
