# MCP Devbox configuration reference

This is the canonical reference for administrator-controlled configuration. Other
runbooks should document only the variables specific to their workflow and link here
for the complete inventory.

MCP Devbox loads policy once at startup. An agent cannot change roots, mode, command
allowlists, authentication, integration credentials, or isolation posture through an
MCP tool. Use `read-only` by default and `ask` when reviewed writes are required.

## Supported profiles

### Local stdio — read-only

- **Components:** the `mcp-devbox` binary and one or more absolute repository roots.
- **Minimum configuration:** `serve --root <ABSOLUTE_PATH>`; mode defaults to
  `read-only`.
- **Volumes:** none. Outside the production image, durable operational state falls
  back below the primary root at `.agent-memory/state`.
- **Security posture:** no writes, commands, tests, or commits. Reads still pass
  through the jail, secret-path denial, content redaction, and audit.
- **Validation:** start the process through an MCP stdio client, call
  `system_runtime_info`, then perform a normal non-secret read.
- **Optional:** explicit audit and observability paths, Brain, and integrations.

```bash
mcp-devbox serve --root /absolute/path/to/repository --mode read-only
```

### Local stdio — ask

- **Components:** the same binary and repository roots.
- **Minimum configuration:** `--mode ask` plus a narrow `--test-cmd` or
  `--allow-cmd` only when those operations are needed.
- **Volumes:** none required; use an administrator-owned state directory when local
  state must survive repository replacement.
- **Security posture:** writes and commands remain jailed and allowlisted. Risky
  actions require an explicit `approve=true`; preview is not approval.
- **Validation:** inspect with `repo_status`, apply a disposable patch, run the
  configured test, and verify the approval boundary before using a real repository.
- **Optional:** GitHub, Coolify, Brain, and a private validation runner.

```bash
mcp-devbox serve --root /absolute/path/to/repository --mode ask \
  --test-cmd "go test ./... -count=1"
```

### Local HTTP

- **Components:** the binary, a repository root, and an HTTP listener.
- **Minimum configuration:** `--http`; then either a recovery bearer supplied by
  `MCP_DEVBOX_TOKEN` or both OAuth variables. The server refuses unauthenticated HTTP.
- **Volumes:** none for a disposable local process. Persist a state root when OAuth
  clients and refresh grants must survive restart.
- **Security posture:** a hostless address binds to loopback. Keep it on loopback
  unless a reviewed TLS/authenticated reverse proxy fronts it.
- **Validation:** check `/healthz`, `/version`, unauthenticated `401` on `/mcp`, and
  authenticated MCP initialization.
- **Optional:** OAuth, console durable state, Brain, and integrations.

```bash
MCP_DEVBOX_TOKEN=REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE \
  mcp-devbox serve --root /absolute/path --mode read-only --http :8765
```

### VPS/Coolify with OAuth

- **Components:** the Docker image, TLS reverse proxy, persistent `/repos` and
  `/state` volumes, and the in-process OAuth authorization server.
- **Minimum configuration:** `MCP_DEVBOX_ROOT=/repos`, a safe mode,
  `MCP_DEVBOX_PUBLIC_URL`, and `MCP_DEVBOX_OAUTH_PASSPHRASE`.
- **Volumes:** `/repos` and `/state`; `/brain` only when Brain is enabled.
- **Security posture:** OAuth is preferred. A static bearer is optional recovery by
  `Authorization` header only. Query-string credentials are rejected. Port `8765`
  stays internal to the platform network.
- **Validation:** `/healthz`, `/version`, OAuth discovery, connector authorization,
  exact-commit smoke, and persistence across one rolling replacement.
- **Optional:** recovery bearer, GitHub, Coolify, Brain, and the private validation
  runner.

### Stable MCP Front Door

- **Components:** the dedicated `mcp-front-door` binary, TLS routing, and one fixed
  operator-owned MCP Devbox backend origin.
- **Minimum configuration:** backend URL, exact protocol version, and exact catalog
  hash. The dedicated image is built with `Dockerfile.front-door`.
- **Volumes:** none. The front door is stateless and must not mount `/repos`, `/state`,
  `/brain`, a Docker socket, or Edge state.
- **Security posture:** it forwards the original OAuth and MCP headers without logging
  or persisting them. The backend remains authoritative for authentication, sessions,
  policy, tools, repositories and Edge operations.
- **Validation:** check `/front-door/healthz`, then `/front-door/readyz`; verify OAuth,
  initialization, session reuse and the exact backend `/version` through the facade.
  Perform backend replacements without redeploying the front-door application.
- **Optional:** shorter probe intervals within the documented bounds. The front-door
  deployment should use a stable branch that advances independently from backend
  `main`.

### Global builder

- **Components:** the VPS profile plus Go, Git, Node/npm in the image and optional
  GitHub/Coolify adapters.
- **Minimum configuration:** use `ask`; configure only the integrations needed.
- **Volumes:** persistent `/repos` and `/state`.
- **Security posture:** no free shell, no force push, no caller-provided Git refspec,
  and no Docker socket in the public MCP container. Consequential writes use a
  preview, single-use plan, approval, state revalidation, narrow execution, and audit.
- **Validation:** use `repo_fetch` and the fast-forward plan pair; verify a local
  commit does not push; then test planned publication/deployment in a disposable
  repository or application.
- **Optional:** GitHub API, Coolify, fixed private validation runner, and Brain.

### Persistent Brain

- **Components:** the normal runtime plus a dedicated absolute Brain root.
- **Minimum configuration:** `MCP_DEVBOX_BRAIN_ROOT=/brain` and a persistent `/brain`
  volume disjoint from every repository root.
- **Volumes:** `/brain` is authoritative; `/state/brain` stores only the console node
  identity associated with the runtime state.
- **Security posture:** agent writes are limited to `working/`; curated notes are
  owner-controlled. Markdown and local Git are truth; the SQLite FTS cache is
  disposable. A malformed or unsafe Brain fails startup.
- **Validation:** `brain_index status`, `brain_context`, and `cmd/brain-smoke` against
  the exact deployed commit.
- **Optional:** none beyond the normal runtime integrations.

### Observability and durable state

- **Components:** the runtime and an administrator-owned state root.
- **Minimum configuration:** none locally. The image sets
  `MCP_DEVBOX_STATE_ROOT=/state`, `MCP_DEVBOX_TASK_ROOT=/state/tasks`, and
  `MCP_DEVBOX_OBSERVABILITY=file`.
- **Volumes:** persist `/state` in production.
- **Security posture:** state should be outside the repository jail. Logs and metrics
  have closed schemas and private permissions; prompts, arguments, results, paths,
  credentials, and raw errors are excluded from observability.
- **Validation:** verify the state layout, ownership, `0700` directories, `0600`
  files, fixed log rotation, and survival across restart.
- **Optional:** explicit observability path and rotation size.

### Control plane + Edge

- **Components:** the VPS control plane, a paired Linux/Parrot/WSL Edge, its signed
  release, local service, private state, and one registered workspace.
- **Minimum configuration:** server `/state` persistence plus the documented signed
  package/onboarding process. Edge identity is not configured through public MCP
  environment variables.
- **Volumes and paths:** server coordination is under `/state/edge` and
  `/state/model-turns`; the real Edge keeps private state under
  `~/.local/state/mcp-edge`, with workspaces under the configured local roots.
- **Security posture:** the ordinary Edge sandbox is networkless; the trusted Linux
  workcell intentionally shares the host network; authorized target-locked actions
  revalidate the private target and VPN route. These are distinct boundaries.
- **Validation:** use `system_runtime_info` for the server and the supported local
  doctor/status commands for the installed Edge. Source release, deployed server, and
  installed device evidence are separate facts.
- **Optional:** local owner-bound Git authority and authorized laboratory metadata.

### Privileged profiles

- **Components:** server-defined fixed profiles and, where required, a separately
  contained executor.
- **Minimum configuration:** none. They are disabled by default.
- **Volumes:** profile-specific; the public MCP container must not receive a Docker
  socket merely to enable them.
- **Security posture:** callers never provide an executable, shell string, URL, or
  arbitrary argv. A reviewed plan and normal mode approval still apply.
- **Validation:** confirm a disabled profile fails closed; enable only the exact
  administrator-approved service names and verify preview output before execution.
- **Optional:** all of this profile.

## Precedence

Flags override environment variables, and environment variables override secure
defaults only where the implementation explicitly supports both forms:

| Setting | Precedence |
|---|---|
| test command | `--test-cmd` → `MCP_DEVBOX_TEST_CMD` → none |
| command allowlist | `--allow-cmd` → `MCP_DEVBOX_ALLOW_CMD` → `git,go,ls,cat` |
| sandbox backend | `--sandbox` → `MCP_DEVBOX_SANDBOX` → `none` |
| observability mode/path/size | matching flag → matching environment variable → observability defaults |
| HTTP recovery bearer | `--http-token` → `MCP_DEVBOX_TOKEN` → absent |

`--root`, `--mode`, `--audit`, `--http`, and the Docker entrypoint variables do not
share one generic precedence rule. The direct binary requires at least one `--root`.
The image translates `MCP_DEVBOX_ROOT` and `MCP_DEVBOX_MODE` into flags before the Go
process starts. Brain, state, task, console timezone, OAuth, GitHub, Coolify, privileged
profiles, and validation-runner connections are environment-only startup inputs.

## Environment and build inventory

The **Required** column means required for the named capability, not for every MCP
Devbox process. “Startup fail” distinguishes eager validation from integrations that
remain unavailable until a tool is called.

### Core runtime and policy

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `MCP_DEVBOX_ROOT` | Docker entrypoint | Image runtime; not secret | `/repos/workspace`; absolute path expected | `/repos`; platform env | Missing uses image default. Invalid root causes startup failure. |
| `MCP_DEVBOX_MODE` | Docker entrypoint/policy | Optional; not secret | `read-only`; `read-only`, `ask`, `allow` | `read-only`; platform env | Missing is safe. Unsupported mode is rejected during policy initialization. |
| `MCP_DEVBOX_TEST_CMD` | test runner | Required only for `run_tests`; not secret | none; argv parsed without a shell | `go test ./... -count=1`; platform env | Missing disables a useful test command. Unsafe/unallowlisted execution is denied. |
| `MCP_DEVBOX_ALLOW_CMD` | command policy | Optional; not secret | secure list `git,go,ls,cat`; comma-separated basenames | `git,go`; platform env | Missing keeps the secure list. Invalid/unsafe commands remain denied by policy. |
| `MCP_DEVBOX_SANDBOX` | L3 runner selection | Optional; not secret | `none`; `none`, `docker`, `nsjail`, `gvisor` | `none`; platform env | Unknown value fails startup. A named backend does not bypass availability checks. |
| `MCP_DEVBOX_SANDBOX_IMAGE` | Docker sandbox | Only with Docker backend; not secret | `golang:1.26-alpine` | a reviewed pinned image; platform env | Missing uses default. Runtime creation fails if the image/backend is unavailable. |
| `MCP_DEVBOX_ADMIN_TOKEN` | `mcp-devbox grant` CLI | Only as an alternative to `--admin-token`; secret | none | inject into the local operator process only | Missing requires the flag. It is not a daemon startup setting; the daemon generates its own loopback token. |

### HTTP, OAuth, and console

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `MCP_DEVBOX_TOKEN` | HTTP recovery auth and smoke clients | Required when HTTP has no OAuth; secret | none; long random bearer | `REPLACE_WITH_LONG_RANDOM_RECOVERY_VALUE`; secret manager | HTTP refuses to start if no bearer and no OAuth. Never put it in a URL. |
| `MCP_DEVBOX_PUBLIC_URL` | OAuth issuer/resource | Required with passphrase; not itself secret | none; HTTPS base URL, except localhost may use HTTP | `https://mcp.example.com`; platform env | Both OAuth vars absent disables OAuth. Only one set fails startup. Invalid issuer fails startup. |
| `MCP_DEVBOX_OAUTH_PASSPHRASE` | OAuth owner login | Required with public URL; secret | none; strong passphrase | `REPLACE_WITH_LONG_OWNER_PASSPHRASE`; secret manager | Half-configuration or invalid provider setup fails startup. |
| `MCP_DEVBOX_OAUTH_CLIENT_STORE` | OAuth DCR persistence | Optional; sensitive state path | under state root when configured; absolute override | `/state/oauth-clients.json`; `/state` volume | Missing with a state root uses the default; without a state root DCR is memory-only. Invalid/unwritable store fails OAuth startup. |
| `MCP_DEVBOX_OAUTH_REFRESH_STORE` | refresh-token persistence | Optional; secret-bearing state path | under state root when configured; absolute override | `/state/oauth-refresh.json`; `/state` volume | Missing with a state root uses the default; without it refresh grants are memory-only. Invalid/unwritable store fails OAuth startup. |
| `CONSOLE_TIMEZONE` | console presentation | Optional; not secret | `America/Bogota`; valid IANA name or `UTC` | `America/Bogota`; platform env | Empty uses default. Invalid or ambiguous timezone fails startup. |

### Stable MCP Front Door service

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `MCP_FRONT_DOOR_BACKEND_URL` | fixed backend origin | Required; sensitive topology, not a credential | HTTPS origin without user info, query, fragment or path; loopback HTTP only for local validation | `https://mcp-backend.example.com`; platform env | Missing or unsafe origin fails startup. It is never accepted from a request. |
| `MCP_FRONT_DOOR_EXPECTED_PROTOCOL` | compatibility gate | Required; not secret | exact MCP protocol date | `2024-11-05`; platform env | Missing or malformed value fails startup. A backend mismatch makes the facade unready. |
| `MCP_FRONT_DOOR_EXPECTED_CATALOG_HASH` | compatibility gate | Required; not secret | `sha256:` plus 64 lowercase hexadecimal characters | read from the approved backend `/version`; platform env | Missing or malformed value fails startup. A mismatch blocks new proxied requests. |
| `MCP_FRONT_DOOR_ADDR` | front-door listener | Optional; not secret | `0.0.0.0:8765`; valid host:port | platform env | Invalid address fails startup. Keep the port behind TLS routing. |
| `MCP_FRONT_DOOR_PROBE_INTERVAL` | compatibility probe | Optional; not secret | `1s`; 250 ms–1 minute | platform env | Invalid or out-of-range value fails startup. Do not increase it to hide rollout failures. |
| `MCP_FRONT_DOOR_PROBE_TIMEOUT` | compatibility probe | Optional; not secret | `3s`; 250 ms–10 seconds | platform env | Invalid or out-of-range value fails startup. |

### Durable state, Brain, and observability

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `MCP_DEVBOX_STATE_ROOT` | all durable server state | Recommended in production; sensitive path | local fallback `<primary-root>/.agent-memory/state`; absolute path | `/state`; persistent volume outside the repository jail | Missing uses local fallback. Relative/NUL path fails startup. |
| `MCP_DEVBOX_TASK_ROOT` | durable task journal | Optional; sensitive path | none outside image; image `/state/tasks`; absolute path | `/state/tasks`; `/state` volume | Missing disables the task journal. Invalid path or open failure fails startup. |
| `MCP_DEVBOX_BRAIN_ROOT` | Brain | Required to enable Brain; sensitive path | unset/disabled; absolute and disjoint from repo roots | `/brain`; dedicated volume | Missing leaves Brain tools registered but unavailable. Invalid source, permissions, overlap, Git, or index state fails startup. |
| `MCP_DEVBOX_OBSERVABILITY` | structured events | Optional; not secret | library default `stderr`; image `file`; `off`, `stderr`, `file`, `both` | `file`; platform env | Missing uses the applicable default. Invalid mode fails startup. |
| `MCP_DEVBOX_OBSERVABILITY_PATH` | JSONL sink | Required only for an explicit path; sensitive path | in file/both mode defaults to `<state>/logs/observability.jsonl`; absolute path | `/state/logs/observability.jsonl`; `/state` volume | Invalid, unsafe, or unwritable path fails startup. |
| `MCP_DEVBOX_OBSERVABILITY_MAX_BYTES` | log rotation | Optional; not secret | `16777216`; integer 1 MiB–1 GiB | `16777216`; platform env | Invalid or out-of-range value fails startup. |

### GitHub adapter

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `GITHUB_TOKEN` | GitHub API and HTTPS publication | Required to enable adapter; secret | none; fine-grained token | `REPLACE_WITH_FINE_GRAINED_GITHUB_VALUE`; secret manager | Missing disables the adapter. Incomplete owner/type makes GitHub tools fail closed when called. |
| `GITHUB_OWNER` | owner boundary | Required with token; not secret | none; exact user/org login | `example-owner`; platform env | Missing leaves client unconfigured and tools fail closed. |
| `GITHUB_OWNER_TYPE` | API routing | Required by documented setup; not secret | constructor default `user`; `user` or `org` | `user`; platform env | Invalid value makes the client unconfigured and tools fail closed. |
| `GITHUB_DEFAULT_VISIBILITY` | repo creation | Optional; not secret | `private`; `private` or `public` | `private`; platform env | Missing stays private. Invalid requested visibility is rejected. |

### Coolify adapter

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `COOLIFY_URL` | Coolify API | Required to enable adapter; not secret | none; reviewed base URL | `https://coolify.example.com`; platform env | Missing disables adapter. |
| `COOLIFY_API_TOKEN` | Coolify API | Required with URL; secret | none | `REPLACE_WITH_COOLIFY_API_VALUE`; secret manager | Missing makes Coolify operations fail closed when called. |
| `COOLIFY_ALLOWED_APPS` | app boundary | Optional; not secret | no extra app allowlist; comma-separated UUIDs | `app-uuid`; platform env | Missing leaves configured platform scope in effect. A non-allowed app is rejected. |
| `COOLIFY_SERVER_UUID` | app creation | Required for creation; not secret | none | `server-uuid`; platform env | Missing does not stop base runtime; creation preview fails. |
| `COOLIFY_PROJECT_UUID` | app creation | Required for creation; not secret | none | `project-uuid`; platform env | Missing does not stop base runtime; creation preview fails. |
| `COOLIFY_ENVIRONMENT_NAME` | app creation | One of name/UUID required; not secret | none | `production`; platform env | If both selectors are absent, creation preview fails. |
| `COOLIFY_ENVIRONMENT_UUID` | app creation | One of UUID/name required; not secret | none | `environment-uuid`; platform env | If both selectors are absent, creation preview fails. |
| `COOLIFY_ALLOWED_DOMAINS` | domain boundary | Optional; not secret | no caller-selectable domains unless policy permits; comma-separated suffixes | `example.com`; platform env | A requested domain outside the list is rejected. |
| `COOLIFY_GITHUB_APP_UUID` | private repo source | Optional; sensitive identifier | public repository endpoint when absent | `github-app-uuid`; platform env | Missing preserves public-source behavior. Invalid source fails the platform request. |
| `COOLIFY_DESTINATION_UUID` | managed validation runner | Required only for managed runner creation; not secret | none | `destination-uuid`; platform env | Preview fails when absent. |
| `COOLIFY_ALLOWED_MOUNTS` | managed validation runner | Required only for managed runner creation; sensitive host layout | exactly three semicolon-separated reviewed mounts | administrator-owned exact mount set; secret/private platform env | Preview fails unless Docker socket, `/repos`, and pnpm-store mounts match the closed contract. Never expose it as agent input. |

### Privileged profiles and validation connection

| Name | Component | Required / secret | Default and valid values | Example and persistence | Missing or invalid effect |
|---|---|---|---|---|---|
| `MCP_DEVBOX_PRIVILEGED_TASKS` | fixed privileged profiles | Required only to enable; not secret | disabled; only case-insensitive `true` enables | `false`; platform env | Missing/other value stays disabled. |
| `MCP_DEVBOX_PRIVILEGED_SERVICES` | service profiles | Optional; not secret | empty; comma-separated reviewed names | `mcp-devbox`; platform env | Missing permits no service-specific target. Unsafe names are rejected. |
| `MCP_DEVBOX_PRIVILEGED_TIMEOUT` | profile execution | Optional; not secret | `2m`; positive Go duration | `2m`; platform env | Invalid/non-positive value fails startup. |
| `MCP_DEVBOX_VALIDATION_RUNNER_URL` | public MCP to private runner | Required to enable runner calls; sensitive internal URL | none | `http://validation-runner:8787`; private platform env | Missing leaves validation tools unavailable. Invalid/unreachable endpoint fails the call, not base startup. |
| `MCP_DEVBOX_VALIDATION_RUNNER_TOKEN` | runner authentication | Required with URL; secret | none | `REPLACE_WITH_SHARED_RUNNER_VALUE`; secret manager on both services | Missing leaves validation tools unavailable; runner itself requires at least 32 characters and otherwise fails startup. |

### Private validation-runner service

| Name | Required / secret | Default and valid values | Missing or invalid effect |
|---|---|---|---|
| `MCP_DEVBOX_VALIDATION_RUNNER_ADDR` | Optional; not secret | `:8787` | Missing uses default listener. |
| `MCP_DEVBOX_VALIDATION_RUNNER_ROOT` | Required; sensitive path | absolute container path, normally `/repos` | Missing, relative, non-directory, or unsafe resolution fails runner startup. |
| `MCP_DEVBOX_VALIDATION_RUNNER_HOST_ROOT` | Required; sensitive host path | absolute Docker-host path | Missing/relative fails runner startup. |
| `MCP_DEVBOX_VALIDATION_RUNNER_IMAGE` | Optional; not secret | `node:22-alpine` | Missing uses default; unavailable image fails a job. |
| `MCP_DEVBOX_VALIDATION_RUNNER_STORE` | Optional; not secret | `mcp-devbox-pnpm-store` | Missing uses default. |
| `MCP_DEVBOX_VALIDATION_RUNNER_USER` | Optional; not secret | `10001:10001` | Missing uses non-root default. Use only a reviewed numeric UID:GID. |
| `MCP_DEVBOX_VALIDATION_RUNNER_TIMEOUT` | Optional; not secret | `8m`; positive duration | Missing or invalid value falls back to `8m`. |

### Build and live identity

| Name | Component | Secret | Default / precedence | Effect |
|---|---|---|---|---|
| `GIT_SHA` | Docker build arg | no | `unknown` | Baked into the binary commit with ldflags and intentionally busts the relevant build cache. |
| `BUILD_TIME` | Docker build arg | no | `unknown` | Baked build timestamp metadata. |
| `BUILD_GOMAXPROCS` | console and Go build arg | no | `1` | Caps logical CPU used by builds; dedicated builders may raise it deliberately. |
| `BUILD_GO_PARALLELISM` | Go build arg | no | `1` | Passed to `go build -p`. |
| `BUILD_UV_THREADPOOL_SIZE` | console build arg | no | `1` | Caps Node/libuv worker concurrency during console assembly. |
| `MCP_DEVBOX_COMMIT` | runtime build identity fallback | no | checked before `SOURCE_COMMIT` | Overrides unstamped commit identity at process startup. |
| `SOURCE_COMMIT` | Coolify runtime identity fallback | no | checked after `MCP_DEVBOX_COMMIT` | Used only when the binary was not adequately stamped. Read live identity from `/version` or `system_runtime_info`. |

Build-only CI variables, test toggles, and runtime-generated Edge variables are not
administrator configuration for the public control plane. They belong to their
workflow or private runtime contract and must not be copied into production merely
because they appear in source.

## Ports, routes, paths, and volumes

| Item | Purpose | Persistence / ownership | Jail and backup posture |
|---|---|---|---|
| port `8765` | internal HTTP listener and healthcheck | container-only; Traefik/reverse proxy routes to it | do not publish directly on the VPS firewall |
| `/front-door/healthz` | stateless facade liveness | none | use for front-door container health; independent from backend readiness |
| `/front-door/readyz` | compatible backend readiness | memory-only probe state | `503` blocks new requests while accepted requests drain |
| `/front-door/version` | bounded facade and last backend identity | memory-only probe state | diagnostic only; public `/version` remains the proxied backend identity |
| `/mcp` | authenticated MCP stream and JSON-RPC | no filesystem persistence | OAuth preferred; bearer header recovery only |
| `/healthz` | bounded liveness/build identity | none | public only according to deployment policy |
| `/version` | safe live version, commit, protocol, tool count, catalog hash | none | source of live deployment identity; do not hardcode it in operational docs |
| OAuth discovery and `/oauth/*` | discovery, registration, authorization, token exchange | client/refresh stores under `/state` when configured | codes/access tokens remain memory-only; never expose store files to agents |
| `/repos` | repository jail root in global-builder mode | persistent, runtime UID/GID `10001:10001` in image | agent-visible by design; back up repositories according to project policy |
| `/state` | OAuth stores, audit, observability, metrics, tasks, results, model turns, Edge coordination, console state | persistent, private; image prepares subdirectories `0700`, files are expected `0600` | must stay outside the repository jail; back up durable authority/state, not transient cache blindly |
| `/state/results` | bounded redacted `result_ref` payloads | persistent when result continuity matters | sensitive operational data; expire/clean by store policy |
| `/state/logs` | audit and observability segments | persistent; private fixed rotation | back up when audit retention requires it |
| `/state/telemetry` | content-free aggregate SQLite metrics | persistent but reconstructability is limited | retain per operational policy; never treat it as request-content evidence |
| `/state/model-turns` | durable model-turn coordination | persistent | private control-plane state; never expose to repository tools |
| `/state/edge` | paired Edge/control operations | persistent | private authority state; back up and protect separately |
| `/state/console` | console sessions | persistent when login continuity matters | private; never agent-writable |
| `/state/brain` | Brain console node identity | persistent | private runtime identity, distinct from Brain truth |
| `/brain` | Brain Markdown truth, local Git, and disposable `.cache` | dedicated persistent volume; dirs `0700`, files `0600`, UID/GID `10001:10001` in image | outside the repository jail; back up `.git`, `.gitignore`, `curated`, `working`; `.cache` is disposable |
| `~/.local/state/mcp-edge` | installed Edge identity, registry, jobs, results, local Git authority | persistent, owner-only; credential files `0600` | never mounted into model workcells or returned through tools; back up before lifecycle changes |
| `/opt/mcp-devbox/releases/<release>` and `/opt/mcp-devbox/current` | signed immutable Edge releases and active link | root-owned package/updater state | replace only through the signed installer/updater; source release and installed release require separate evidence |

No secret store, OAuth store, Edge identity, local Git credential, Docker socket, or
host-private state path should be exposed as a repository alias or normal agent-writable
mount.

## Secret handling

Secret values include recovery and runner bearers, OAuth passphrases and refresh
stores, GitHub/Coolify credentials, the local grant token, Edge private identity, and
local Git transport authority.

- Store them in the Coolify secret manager or an equivalent administrator-controlled
  mechanism.
- Never put them in Git, prompts, repository files, logs, URLs, or command arguments.
- Use unmistakable placeholders in examples.
- Tool responses and audit records must never return tokens or environment values.
- Query-string credentials are rejected; use OAuth or an `Authorization` header.
- Keep `/state`, `/brain`, and Edge private state out of the repository jail.

## Copyable safe examples

The repository deliberately does not use `.env.example`: `.env` and `.env.*` are
secret-denied paths. The safe, non-populated template is
[`config/mcp-devbox.env.sample`](../config/mcp-devbox.env.sample). Copy values from it
into the platform environment/secret manager rather than committing a populated copy.

Minimal local inspection:

```bash
mcp-devbox serve --root /absolute/repository --mode read-only
```

Minimal reviewed local development:

```bash
mcp-devbox serve --root /absolute/repository --mode ask \
  --test-cmd "go test ./... -count=1" \
  --allow-cmd git,go
```

Minimal production posture:

```text
MCP_DEVBOX_ROOT=/repos
MCP_DEVBOX_MODE=read-only
MCP_DEVBOX_STATE_ROOT=/state
MCP_DEVBOX_TASK_ROOT=/state/tasks
MCP_DEVBOX_OBSERVABILITY=file
MCP_DEVBOX_PUBLIC_URL=https://mcp.example.com
MCP_DEVBOX_OAUTH_PASSPHRASE=REPLACE_WITH_LONG_OWNER_PASSPHRASE
MCP_DEVBOX_PRIVILEGED_TASKS=false
```

Switch production to `ask` only when the operator deliberately wants reviewed
patch/test/commit/publication/deployment workflows. `allow` is not a general deployment
recommendation.
