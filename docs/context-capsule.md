# Context Capsule - mcp-devbox

Compact handoff for any AI session. Keep this file short and current.

## Current Goal

L1 + remote connectivity are done and running in production: ChatGPT web connects to
the Coolify/VPS deployment over HTTPS and can operate on repos cloned in the VPS
volume. Current goal: evolve mcp-devbox into a GPT-driven agent tool box that can
safely do work, while a human keeps control of risky operations.

P0 architecture foundations are deployed. The deterministic catalog, centralized
build identity, safe `/version` diagnostics, no-cache headers, `tools.listChanged`
notification, and post-deploy catalog smoke distinguish a stale server from a stale
client catalog.
See `docs/adr/0001-p0-catalog-cache-and-product-foundations.md`,
`docs/baselines/2026-07-12-p0.md`, and `docs/quality-gates.md`. Existing environment
variable names and MCP wire contracts are frozen for compatibility during P0-P3.

P1 catalog modularization is deployed on `main` at commit
`0de426e088466a1421b527f8ce1bf83cb53bd2a9`. Public tool registrations, aliases,
and annotations are declarative under `internal/mcpserver/catalog`; `tools.go` is
composition-only and protected by an AST boundary test. Production and the connected
ChatGPT client both verified 62 tools with catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.

P2 capability service split is deployed on `main` at commit
`ea332d173b4be1908bcf1c1abbe77ece610a6761`. One central `serviceCore` owns
policy, audit, root, runner, redaction, workdir resolution and action plans. Focused
repository, Git, source-hosting, platform and execution capabilities implement the
catalog contracts behind the compatible `Service` facade. Production is healthy and
retains the same 62 tools and deterministic catalog hash.

P3 composition root is deployed on `main` at commit
`dd055e251c455086ddcb02bc302d9f406b05d6ce`. `cmd/mcp-devbox/main.go` delegates
only to `app.Main()`. Focused modules under `internal/app` own command dispatch,
deployed environment contracts, serve option parsing, OAuth, runtime composition,
local grant administration and stdio/HTTP transport lifecycle. Production is healthy
and retains the same 62-tool public surface and catalog hash.

P4 targeted Layer-1 hardening is deployed on `main` at commit
`4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`. Steps 70-76 block command/PATH
spoofing, enforce grant/request bounds, keep documentation state tested, redact audit
paths, and bound HTTP JSON-RPC batches to 128 items. Production is healthy with 62
tools and the unchanged deterministic catalog hash.

P5 deeper testing is deployed on `main` at commit
`4a68ca054a5f077d62a0f887234866673feb7353`. Production is healthy with the same
62 tools and catalog hash. P5 added concurrency, fuzz seeds, package coverage, and
hermetic integration evidence without runtime authority changes.

P6 CI/DevSecOps is active on branch `p6-ci-devsecops`. Foundation and the tested
workflow policy guard are complete; the guard already forced a bounded timeout on the
existing CI job. Next are core CI, security/container workflows, and scheduled fuzzing.
No workflow may use secrets from pull requests or contact production.

Product roadmap (2026-07-13): `docs/product-roadmap.md` defines the complete path
from the Cubethon showcase to universal execution profiles, private PC/WSL/Parrot
edge agents, provider-neutral MiniMax/OpenCode orchestration, and scope-bound
authorized security research. The public console is presentation-only; MCP Devbox
remains the private authority and is not tied to any framework or model provider.

## Vision (updated 2026-06-30)

The agent is ChatGPT itself. ChatGPT drives the loop directly through MCP tools;
mcp-devbox provides the safe hands: jail, policy, redaction, grants, audit, and
mode-gated actions.

Do not reintroduce the old L2 cheap-model worker / `delegate_to_worker` plan and do
not fork opencode or another paid agent loop. The owner wants to use the ChatGPT
session they already pay for, avoid burning Codex/licensed-agent credits, and fall
back to Codex/Claude/opencode only for work that proves impossible through MCP.

Revised priorities:

1. Agent-first tool UX: better instructions, memory, safe write/test/commit loops.
2. Grants hardening: local-human-only, exact-path, single-use/TTL, raw double-gated.
3. L3 hard isolation: OS sandbox plus egress controls before broad command/disk power.
4. Install/deploy polish: easy repeatable VPS deployment and safer auth front doors.

`docs/features.md` is explicitly marked SUPERSEDED for the old worker plan. This
capsule is the current source of truth for direction.

Image policy: keep the runtime capable, not minimal. The Docker image intentionally
keeps Go + git + Node/npm because the VPS box is meant to run tests/builds and grow
into a broader, audited builder. Do not shrink it into a bare runtime that cannot do
useful work.

Security consequence: every new capability must be a deliberate allowlisted and
audited tool with explicit jail scoping. There is no free terminal before L3.

## Current State

Layer 1, v0.2 HTTP transport, Docker/Coolify deploy, and ephemeral human access
grants are implemented and live. Module: `github.com/charle-z/mcp-devbox` (Go 1.26).
Main branch: `main`.

Production:

- Host: `https://mcp-devbox-charlez.duckdns.org`
- MCP endpoint: `/mcp`
- Preferred ChatGPT auth: OAuth with DCR, public client, scope `mcp`.
- Legacy fallback: `/mcp?key=<MCP_DEVBOX_TOKEN>`.
- Runtime root: `/repos`
- Default mode: `read-only`; global-builder production should use `ask`
- Repos live in the persistent `/repos` volume.

Transport:

- stdio for local clients.
- HTTP `POST /mcp` JSON-RPC, bearer or `?key=` token required.
- `/healthz` for liveness and `/version` for safe build/catalog identity.
- Dynamic HTTP responses disable caching and include live commit/catalog headers.
- Authenticated `GET /mcp` returns a minimal SSE stream; unauthenticated `GET /mcp`
  returns 401.
- HTTP `initialize` responses include `Mcp-Session-Id`; later POSTs may send that
  header and are accepted.
- Same Policy/Service/redaction path for both transports; no duplicated security checks.
- OAuth 2.1: in-process AS + resource server in `internal/oauth`. Enable with env
  `MCP_DEVBOX_PUBLIC_URL` + `MCP_DEVBOX_OAUTH_PASSPHRASE`; discovery (RFC 9728/8414),
  DCR (7591), PKCE S256, refresh rotation, audience-bound tokens. Optional
  `MCP_DEVBOX_OAUTH_CLIENT_STORE` persists only DCR public client registrations.
  `MCP_DEVBOX_OAUTH_REFRESH_STORE` optionally persists rotating refresh tokens with
  mode 0600 so ChatGPT can survive redeploys without repeating owner login. Access
  tokens and authorization codes remain in-memory only. Static bearer/`?key=` remains
  available as fallback.
  See `docs/oauth.md`.

Ephemeral grants:

- Secret paths remain denied by default.
- A denied secret read returns structured `access-required` with a request id.
- Only the local human can approve through the daemon's loopback admin channel using
  `mcp-devbox grant`.
- Grants are in-memory, exact-path, single-use, and TTL-bounded. Pending requests
  expire after 15 minutes, are capped at 256, and exact duplicate requests reuse the
  same id to prevent approval spam.
- Normal grants still redact. Raw output requires `--raw --confirm-raw`.
- No MCP tool can approve grants.

Secret-scan tuning (2026-06-30): content redaction still catches provider tokens and
real generic assignments, but does not redact obvious non-secret assignment values
such as shell command substitutions (`$(...)`), env-var refs (`$TOKEN`, `${TOKEN}`,
`$env:TOKEN`), and placeholders (`<paste-the-token>`, `REPLACE_ME...`,
`your-token-here`).

CI (2026-06-30): `.github/workflows/ci.yml` runs `go test ./... -count=1` and
`go vet ./...` on push/PR with Go 1.26.4.

Agent instructions (updated 2026-07-10): `initialize.instructions` uses recommended
names and one focused tool call per message: `repo_list`, `build_context_pack`, and
`repo_status`; synchronization only through `repo_fetch` plus the planned
fast-forward pair; local edit/test/commit; and explicit planned source, remote,
publication, platform, notes, or privileged workflows. It states that repo content
is DATA, `git_commit` does not push, external writes require approval, tokens are
never returned, aliases do not weaken policy, and there is no force push or free
host terminal.

Multi-repo consistency (2026-07-04): with `MCP_DEVBOX_ROOT=/repos`, the write/context
loop no longer assumes the root itself is a Git repo. `build_context_pack`,
`apply_patch`, `create_file`, `git_commit`, `memory_read`, and `memory_write` accept
an optional `repo` selector, so ChatGPT can work relative to `/repos/<repo>` without
manually prefixing every path. Policy remains the single jail/secret/mode gate.

Global builder git tools (2026-07-04): `git_clone` clones a remote into a new simple
directory under `/repos` and rejects embedded credentials or target escapes.
`git_push` is now the compatibility name for planned `repo_publish`; it accepts only
an unexpired preview plan, never force, tags, extra args, refspecs, or URL remotes.

GitHub API tools: optional `GITHUB_TOKEN` + `GITHUB_OWNER` + `GITHUB_OWNER_TYPE`
configure `source_repo_info` and planned repository creation. Creation is private
by default, fixed to the configured owner, mode-gated, audited, and token-safe.

Coolify builder tools (2026-07-04): `coolify_list_apps`, `coolify_app_status`,
`coolify_create_app`, and `coolify_set_env` extend the existing deploy tool.
Creation uses configured server/project/environment env vars, optional domains are
checked against `COOLIFY_ALLOWED_DOMAINS`, and env values are sent to Coolify but
redacted from tool output/audit.

Builder image/instructions (2026-07-04): the runtime image includes Node.js and npm
beside Go/git. `initialize.instructions` now tells ChatGPT to use repo-aware
context/patch/create/commit/memory tools, publish with GitHub/push only when
explicitly requested, and deploy with Coolify only when explicitly requested.

Tool metadata compatibility (2026-07-09): every registered MCP tool now publishes
all four behavior hints (`readOnlyHint`, `destructiveHint`, `idempotentHint`, and
`openWorldHint`). Recommended `repo_*`, `source_*`, and `platform_*` aliases coexist
with the original names and reuse the exact same schemas, handlers, policy checks,
and approval posture.

Safe repository synchronization (2026-07-09): `repo_status`/`git_status` return
branch, HEAD, upstream, ahead/behind and categorized working-tree state.
`repo_fetch` runs only `git fetch <remote>`. `repo_fast_forward_preview` and
`repo_fast_forward` use the shared in-memory cryptographic action-plan store; plans
are short-lived, exact-state-bound and single-use, and execution revalidates the
jailed repo, attached branch, clean tree, HEAD, upstream target and fast-forward
relationship before `git merge --ff-only`.

Planned source hosting (2026-07-09): `source_repo_info` returns existence,
visibility, default branch, credential-free clone URL and viewer permission for the
fixed configured GitHub owner. Repository creation is now a two-step
`source_repo_create_preview` -> `source_repo_create` flow with exact expiring plans,
private-by-default visibility and an existence recheck. `repo_remote_preview` and
`repo_remote_set` similarly plan and revalidate credential-free GitHub remotes,
restricted to `GITHUB_OWNER`. The legacy `github_*` names remain registered on the
same safe handlers.

Planned publication (2026-07-09): `repo_publish_preview` inspects a clean attached
current branch, a named credential-free GitHub remote, and the exact remote branch
SHA; it rejects behind/diverged state and creates an expiring single-use plan.
`repo_publish` revalidates branch, HEAD, tree, remote URL and remote branch before
running one generated `git push` (with `-u` only for the initial branch). Force,
mirror, tags, arbitrary refspecs, URL remotes and extra arguments are not expressible.
Legacy `git_push` invokes the same planned execution handler.

Planned Coolify operations (2026-07-10): `platform_apps_list` and
`platform_app_status` return safe application summaries. Application creation is
`platform_app_create_preview` -> `platform_app_create`, validating the configured
server/project/environment, GitHub owner, domain allowlist, port, build pack,
healthcheck and required environment-variable names (never values). Deployment is
`platform_deploy_preview` -> `platform_deploy`, bound to the app repository, branch
and expected commit. Both write flows use expiring single-use plans and revalidation;
legacy Coolify names share the same handlers.

No-cache deployments (2026-07-12): `platform_deploy_without_cache_preview` ->
`platform_deploy_without_cache` uses a separate expiring single-use plan and requests
Coolify's existing deploy endpoint with `force=true`. It reuses the same application
allowlist, repository/branch/commit revalidation, approval gate, audit, redaction, and
token handling as normal deployments; the ordinary flow remains explicitly
`force=false`.

GitHub publication and private Coolify sources (2026-07-11): `repo_publish` uses
credential-safe HTTPS authentication for configured owner-bound GitHub remotes
without persisting a token in the remote URL. `COOLIFY_GITHUB_APP_UUID` selects the
configured Coolify GitHub App source and routes application creation through
`/api/v1/applications/private-github-app`; when it is unset, the public repository
endpoint remains the backwards-compatible default. Production verified this path by
creating and deploying a private GitHub repository through the Coolify source.

Controlled privileged profiles (2026-07-10): `privileged_task_preview` and
`privileged_task_execute` expose only server-defined profiles and are disabled by
default (`MCP_DEVBOX_PRIVILEGED_TASKS=true` enables them). The client cannot provide
an executable, argv or shell string. Previews show the exact command, jailed cwd,
network/filesystem scope, effect, risk and a two-minute single-use plan. Execution
reuses mode approval, audit and timeouts; service names require
`MCP_DEVBOX_PRIVILEGED_SERVICES`. Docker profiles preview but fail securely in the
public MCP architecture rather than exposing the Docker socket. Go verification
profiles require an available sandbox so their no-network posture is enforceable.

Persistent user notes (2026-07-10): free-form Markdown notes are separate from
structured `memory_write` sections and live at `/repos/.agent-memory/notes` when
the configured root is `/repos`. `notes_list` and `notes_read` expose safe metadata
and redacted content. `notes_write_preview` -> `notes_write` supports create or
append only, with validated slugs, a 64 KiB limit, symlink defense, no overwrite,
content-hash revalidation, timestamped appends and expiring single-use plans. Notes
are not automatically committed into child project repositories.

## What Works

- Policy (`internal/policy`): path jail for fs and commands, symlink/traversal/UNC/
  sibling-prefix protection, secret path deny, content redaction, command allowlist,
  destructive/injection blocking, in-memory read grants, immutable runtime policy.
- Audit (`internal/audit`): append-only JSONL, secret-scrubbed, concurrency-safe.
- Tools (`internal/tools`): every registered tool has a schema, description, four
  annotations, handler and tests. See `docs/tools.md` for the canonical complete
  table, compatibility aliases, exact effects, and workflows.
- Writes: `apply_patch` is patch-first and validates with `git apply --check`;
  `create_file` refuses overwrite and goes through the same patch pipeline. Both
  accept an optional `repo` selector for `/repos/<repo>` workspaces.
- Commands: `run_command` and `run_tests` are allowlist-only, mode-gated, no shell,
  output redacted. Both accept an optional jailed `cwd`. P4 additionally requires bare
  executable names and resolves them to canonical absolute paths outside configured
  workspace roots, preventing `./git` and hostile workspace `PATH` spoofing.
- L3 status: `sandbox_status` reports the current sandbox backend state. It is
  diagnostic only; the default is unavailable and `run_command` remains L1
  allowlist-only.
- Git: `git_status`, `git_diff`, and `git_commit` accept an optional jailed `repo`
  selector for `/repos/<repo>` workspaces. `git_commit` stages and commits locally
  but does not push.
- Memory: `memory_read` and `memory_write` accept an optional `repo` selector.
  `memory_write` updates only the structured sections `current-task`, `plan`,
  `decisions`, and `reflections` under `.agent-memory/`; it uses the Policy write
  gate, requires approval in `ask`, and redacts before persisting.
- Deploy: `Dockerfile`, `.dockerignore`, and `docs/deploy-coolify.md` support
  Coolify/Traefik with `MCP_DEVBOX_TOKEN` supplied only by env/secrets.

## Important Files

| File | Why |
|---|---|
| `AGENTS.md` | Operating rules + security invariants. |
| `.agent-memory/handoffs/latest.md` | Prioritized backlog and per-step handoff. |
| `docs/security.md` | Threat model and secure-by-default invariants. |
| `docs/design.md` | Architecture and layer decisions. |
| `docs/connect-remote.md` | ChatGPT/local client setup and real-world connector notes. |
| `docs/deploy-coolify.md` | VPS/Coolify deployment guide. |
| `docs/product-roadmap.md` | Cubethon, universal profiles, edge, orchestrator, and authorized-security roadmap. |
| `docs/security-engagements.md` | Generic private engagement authority and edge-enforced security workflow. |
| `docs/open-source-release.md` | Proposed public/private boundary and release-readiness checklist. |

## Commands

```bash
go test ./... -count=1
go vet ./...
go build ./...
gofmt -l $(git ls-files '*.go')

go run ./cmd/mcp-devbox serve --root <ABS_PATH> --mode read-only
go run ./cmd/mcp-devbox serve --root <ABS_PATH> --mode ask \
  --test-cmd "go test ./..." --http :8765

mcp-devbox grant --admin http://127.0.0.1:<PORT> --admin-token <TOKEN> \
  [--ttl 5m] [--raw --confirm-raw] <REQUEST_ID>
```

Windows verification when Go is not installed system-wide: use an official
temporary Go 1.26 SDK or the official `golang:1.26` container. Keep SDKs/caches
outside the repository and do not commit generated binaries.

```powershell
$env:PATH = "$env:TEMP\mcp-devbox-go-sdk\go\bin;" + $env:PATH
$env:GOCACHE = "$env:TEMP\mcp-devbox-go-cache"
```

Container/Coolify env:

- `MCP_DEVBOX_TOKEN`
- `MCP_DEVBOX_ROOT`
- `MCP_DEVBOX_MODE`
- `MCP_DEVBOX_TEST_CMD`
- `MCP_DEVBOX_ALLOW_CMD`
- `MCP_DEVBOX_PUBLIC_URL`
- `MCP_DEVBOX_OAUTH_PASSPHRASE`
- `MCP_DEVBOX_OAUTH_CLIENT_STORE` (recommended: `/state/oauth-clients.json` on a
  persistent `/state` volume outside `/repos`)
- `MCP_DEVBOX_OAUTH_REFRESH_STORE` (recommended: `/state/oauth-refresh.json`)
- `GITHUB_TOKEN` (optional, for GitHub tools)
- `GITHUB_OWNER`
- `GITHUB_OWNER_TYPE` (`user` or `org`)
- `GITHUB_DEFAULT_VISIBILITY` (`private` default, or `public`)
- `COOLIFY_URL` (optional, for Coolify tools)
- `COOLIFY_API_TOKEN`
- `COOLIFY_ALLOWED_APPS` (optional app uuid allowlist)
- `COOLIFY_SERVER_UUID`
- `COOLIFY_PROJECT_UUID`
- `COOLIFY_ENVIRONMENT_NAME` or `COOLIFY_ENVIRONMENT_UUID`
- `COOLIFY_ALLOWED_DOMAINS` (optional domain suffix allowlist)
- `COOLIFY_GITHUB_APP_UUID` (optional Coolify GitHub App source for private repos)
- `MCP_DEVBOX_VALIDATION_RUNNER_URL` and `MCP_DEVBOX_VALIDATION_RUNNER_TOKEN`
  (optional private fixed-profile pnpm runner; never publicly routed)
- `MCP_DEVBOX_PRIVILEGED_TASKS` (`true` explicitly enables fixed profiles; default disabled)
- `MCP_DEVBOX_PRIVILEGED_SERVICES` (optional approved service names)
- `MCP_DEVBOX_PRIVILEGED_TIMEOUT` (optional, default `2m`)

## Production Grant Approval

Secret reads return `access-required` plus a `request_id`. To approve:

1. Coolify app logs: find the printed `ACCESS REQUIRED ... mcp-devbox grant --admin
   http://127.0.0.1:<port> --admin-token <tok> --ttl 5m <request_id>` command.
2. Coolify app terminal, inside the container: run that exact command.
3. Add `--raw --confirm-raw` only when the human explicitly wants unredacted secret
   output.

The admin channel is loopback-only and must stay that way.

## Next Steps

1. Execute `specs/002-deeper-testing/tasks.md` in order: race baseline, deterministic
   concurrency, fuzz seeds, coverage gate, and hermetic integration matrix.
2. Keep P5 runtime-neutral and preserve the 62-tool catalog/hash.
3. Close and release P5 before starting P6 CI/DevSecOps on a fresh branch.
4. Keep console, asset broker, universal profiles, and edge-agent work in separate
   specs/phases; PC/WSL edge validation remains pending the owner machine.

Publication now exists only through the planned `repo_publish_preview` /
`repo_publish` flow; `git_push` is the identical compatibility handler.

## Known Risks / Debt

- The category exists; the differentiator is security posture, memory, and
  agent-first tooling.
- ChatGPT remote access exposes a security tool to the internet path; token/auth,
  reverse-proxy gates, and policy all matter.
- `?key=` is practical for ChatGPT but can leak through URL logs/history; rotate if
  exposed and prefer an extra front gate such as Cloudflare Access or Traefik auth.
- L3 is the genuinely hard layer. Wrap proven tech; do not invent a sandbox.

## Last Verified

Date: 2026-07-13. P4 is deployed and healthy on `main` at commit
`4a96307925751cf7fbe7a4f8eb801f86c8edc3ad`. Production reports 62 tools and
deterministic catalog hash
`sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c`.
P5 deeper testing is complete and merge-ready on `p5-deeper-testing`; its baseline
records concurrency, fuzz, coverage, integration, and the CGO-blocked race handoff.
