# Deploy MCP Devbox on Coolify

This runbook covers the Coolify flow only. The complete variable, default, path,
permission, volume, and secret inventory is canonical in
[`configuration.md`](configuration.md). The technical security model is
[`security.md`](security.md).

The production posture is:

- internal HTTP listener on port `8765`;
- TLS and routing through Coolify/Traefik;
- OAuth preferred for ChatGPT;
- optional static bearer as **header-only recovery**;
- query-string credentials rejected;
- persistent `/repos` and `/state` volumes;
- optional persistent `/brain` volume;
- non-root runtime;
- no Docker socket in the public MCP application.

## Prerequisites

- a Coolify server with a working TLS reverse proxy;
- a Git repository and branch Coolify can build;
- a stable public HTTPS hostname;
- persistent storage for repositories and state;
- an owner-controlled secret manager in Coolify;
- a reviewed runtime mode, normally `read-only` or `ask`.

GitHub and Coolify integrations are optional. They are needed only when the running MCP
server itself must perform the corresponding source-host or deployment operations.
Brain is optional and requires its own `/brain` volume.

## Build configuration

Use the repository Dockerfile. The image:

- builds the console and Go binary;
- runs as the non-root runtime user;
- listens on `0.0.0.0:8765` inside the container;
- declares `/repos`, `/state`, and `/brain` as volume locations;
- exposes `/readyz` as the container healthcheck and keeps `/healthz` as liveness;
- accepts build identity inputs so `/version` can report the exact commit.

Do not publish `8765` directly on the VPS firewall. Let Coolify/Traefik route the HTTPS
origin to the internal port.

## Persistent storage

Mount:

| Container path | Required | Purpose |
|---|---:|---|
| `/repos` | yes | repository jail and project data |
| `/state` | yes | OAuth client and refresh stores, audit, observability, tasks, results, console, Edge/control-plane coordination, and other durable runtime state |
| `/brain` | only when Brain is enabled | Brain Markdown truth, local Git, and disposable search cache |

The runtime image expects the non-root application user to own writable data. Preserve
private directory/file modes described in `configuration.md`.

`/state` and `/brain` must remain outside the `/repos` jail. Never expose OAuth stores,
Edge identity, signing material, local Git credentials, or private runtime state as an
agent-writable repository.

## Minimum application settings

Set the application to build the intended branch and expose container port `8765`.
Configure health checking on:

```text
/readyz
```

`/healthz` remains the liveness endpoint. `/readyz` returns `503` as soon as a
replacement starts draining, before the HTTP listener closes, so the proxy stops
routing new sessions while active requests receive their bounded completion window.

Minimum production environment:

```text
MCP_DEVBOX_ROOT=/repos
MCP_DEVBOX_MODE=read-only
MCP_DEVBOX_STATE_ROOT=/state
MCP_DEVBOX_TASK_ROOT=/state/tasks
MCP_DEVBOX_OBSERVABILITY=file
MCP_DEVBOX_PUBLIC_URL=https://mcp.example.com
MCP_DEVBOX_OAUTH_PASSPHRASE=REPLACE_WITH_LONG_OWNER_PASSPHRASE
MCP_DEVBOX_OAUTH_CLIENT_STORE=/state/oauth-clients.json
MCP_DEVBOX_OAUTH_ACCESS_STORE=/state/oauth-access.json
MCP_DEVBOX_OAUTH_REFRESH_STORE=/state/oauth-refresh.json
MCP_DEVBOX_PRIVILEGED_TASKS=false
```

Store the passphrase in Coolify's secret manager. Switch to `ask` only when reviewed
writes, tests, commits, publication, or deployment are required.

The OAuth client and refresh stores must live on persistent `/state`. A rolling
replacement should preserve connector registration and refresh continuity.

## Optional recovery bearer

A static bearer is not required when OAuth is configured. Add it only for a protected
technical recovery client:

```text
MCP_DEVBOX_TOKEN=REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE
```

It authorizes only through `Authorization: Bearer`. Query-string credentials return
`401`, even when the value is correct. Do not place the bearer in the application URL,
Git, logs, screenshots, prompts, or command arguments.

## Optional Brain

Brain is optional. To enable it:

```text
MCP_DEVBOX_BRAIN_ROOT=/brain
```

Attach the dedicated persistent `/brain` volume and verify its ownership before deploy.
Use the Brain runbook for initialization, smoke, backup, restore, and rollback. Do not
infer a current catalog count from historical Brain baselines.

## Optional GitHub authority

Configure GitHub only when the public control plane must inspect or change repositories,
pull requests, checks, or owner-bound publication through the GitHub API/transport.
Use a fine-grained credential with the minimum repositories and permissions needed.

The exact variables and valid values are in `configuration.md`. The configured owner is
an authority boundary; callers cannot substitute another owner or credential.

## Optional Coolify authority

Configure the Coolify adapter only when MCP Devbox must read or perform planned changes
against allowed applications. Keep application/domain/server/project/environment scope
narrow and store the API credential as a secret.

The adapter does not return credential or environment values. Creation and deployment
use preview, single-use plan, approval, revalidation, narrow execution, and audit.

### Managed HTTPS domains for applications

Keep `COOLIFY_ALLOWED_DOMAINS` server-owned and as narrow as possible. For Coolify's
generated sslip hostnames, allow the suffix containing the exact VPS address, for
example `144.225.147.58.sslip.io`; do not allow all of `sslip.io`. A missing allowlist
denies domain selection.

To promote an existing healthy application from an autogenerated HTTP origin, use:

```text
platform_app_domain_update_preview(app=<UUID>, domain=https://<HOST>)
→ review the exact app, current/target domain, commit and finished deployment
→ platform_app_domain_update(plan_id=<PLAN>, approve=true when ask mode requires it)
→ platform_app_status(app=<UUID>)
→ public HTTPS health and identity smoke
```

The execute tool changes only Coolify's `domains` field, sets
`force_domain_override=false`, and does not trigger an application deployment. It
rejects an unhealthy app, an active/non-finished latest deployment, state changed after
preview, a second origin, HTTP, credentials, explicit ports, paths, query strings,
fragments, IP literals and domains outside policy. If Coolify reports success but the
application's bound configuration or deployment changes, MCP Devbox compensates to the
previous domain and reports failure. Certificate issuance and the final public TLS
probe remain separate observed facts; never advertise HTTPS until a normal
certificate-valid request succeeds.

## Optional private validation runner

JavaScript validation that requires a container engine belongs in the separately
private validation runner. The public MCP application **must not mount
`/var/run/docker.sock`** or another rootful engine socket.

The runner accepts only fixed profiles and reviewed mounts. Configure it according to
[`validation-runner.md`](validation-runner.md) and the canonical variable inventory.

## Deploy

1. Save the reviewed environment and secret values.
2. Confirm `/repos` and `/state` are persistent and writable by the runtime user.
3. Add `/brain` only when Brain is enabled.
4. Trigger the normal Coolify deployment from the expected branch/commit.
5. Wait for the application healthcheck.
6. Verify the exact commit before accepting the deployment.

Do not trigger a manual no-cache deployment merely because documentation changed unless
the real platform policy requires it. If Coolify automatically tracks `main`, observe
the automatic deployment and verify identity instead of creating a second deployment.

## Acceptance smoke

Public liveness and identity:

```bash
curl -fsS https://mcp.example.com/healthz
curl -fsS https://mcp.example.com/readyz
curl -fsS https://mcp.example.com/version
```

Authentication boundary:

```bash
curl -i https://mcp.example.com/mcp
curl -i "https://mcp.example.com/mcp?key=REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE"
```

Expected:

- `/healthz` is healthy;
- `/readyz` is ready and becomes unavailable before shutdown during a replacement;
- `/version` reports the intended exact commit and current catalog identity;
- unauthenticated `/mcp` returns `401`;
- query-string credentials return `401`;
- OAuth discovery and authorization routes are reachable through the same HTTPS origin;
- the ChatGPT connector completes OAuth and can call `system_runtime_info`.

When a recovery bearer is configured, test it only through a protected header.

## Replacement test

The managed MCP backend is a durable SQLite singleton. Its catalog-aware coordinator
uses a bounded stop-first replacement because a normal Coolify rolling replacement
starts the candidate while the previous process still holds the writer locks. Coolify
also resolves the configured Git branch during deployment, so rollback uses the fixed
server-owned `backend-rollback-stable` branch rather than treating `git_commit_sha` as
an independently deployable ref. Do not perform a direct rolling deployment of this
managed backend.

After initial acceptance, perform or observe one coordinator-managed replacement and
verify:

1. `/healthz` returns healthy after the replacement;
2. `/version` reports the intended exact commit;
3. existing OAuth client registration remains valid;
4. refresh continuity survives without repeating owner login;
5. repository and state data remain present;
6. Brain remains present only when enabled;
7. no secret values appear in application logs.

The stable Front Door and OAuth discovery remain available during the bounded stop-first
interval. MCP calls may be temporarily unavailable until the new singleton is healthy;
do not report this workflow as zero-downtime. The test distinguishes a healthy new
container from a deployment that silently lost durable authority state.

## Rollback

For the fixed managed backend, use only the catalog-aware coordinator rollback. It
deploys the reviewed previous commit from `backend-rollback-stable` and restores the
application metadata branch to `main`. For other applications, use Coolify's normal
reviewed rollback procedure. After rollback, repeat:

- `/healthz`;
- `/version` exact-commit verification;
- OAuth connector and refresh continuity;
- repository/state/Brain persistence checks;
- bounded log review.

A rollback of the public control plane does not prove or change an installed Edge
release. Verify Edge state separately.

## Troubleshooting

| Symptom | Action |
|---|---|
| Container unhealthy | Inspect bounded application logs, verify port `8765`, `/healthz`, volume ownership, and required OAuth pair. |
| OAuth startup fails | Verify public URL/passphrase are both present and stores are writable on `/state`. |
| OAuth works until redeploy | The client or refresh store is ephemeral or mounted incorrectly. |
| Connector returns `401` | Complete OAuth; bearer recovery requires the `Authorization` header. Query credentials never authorize. |
| `/version` reports the wrong commit | The platform built/deployed another revision or is still completing the rolling replacement. Do not accept it. |
| Repositories disappear | `/repos` is not persistent or is mounted at the wrong path. |
| Brain tools unavailable | Brain is optional; verify the `/brain` volume and `MCP_DEVBOX_BRAIN_ROOT`. |
| GitHub/Coolify tools unavailable | Their optional adapter configuration is absent or invalid. Consult `configuration.md`. |
| Validation requires Docker | Use the private fixed-profile runner; do not add a Docker socket to the public app. |

## Security reminders

- OAuth is preferred; bearer is recovery only.
- Keep secrets in Coolify's secret manager.
- Keep `/state` and `/brain` outside the repository jail.
- Use `read-only` by default and `ask` for reviewed effects.
- Do not expose the local human grant admin channel outside loopback.
- Do not relax application/domain/owner boundaries for convenience.
- Do not describe a healthy VPS deployment as proof of a source release or installed
  Edge state; each requires separate evidence.

## Stable MCP Front Door deployment

Deploy the facade described in [`stable-mcp-front-door.md`](stable-mcp-front-door.md)
as a separate Coolify application from a stable branch. Its container healthcheck must
use `/front-door/healthz`, not the backend `/readyz`, so a backend rollout does not cause
Coolify to replace the facade. Configure one fixed backend origin and pin the approved
protocol and catalog hash. Validate on a temporary hostname before moving the existing
connector hostname through a reviewed, reversible domain operation. Backend `main`
deployments must not automatically redeploy the front door.
