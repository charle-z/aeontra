# mcp-devbox

A **secure-by-default**, local-first MCP server that lets ChatGPT and other AI
agents work on your local repositories — read code, search, apply patches, run
allowed tests/commands, and keep agent-agnostic project memory — **without giving
the agent free reign over your machine**.

## Honest positioning (read this first)

This is **not a new category.** Local-dev MCP servers already exist — most notably
[Desktop Commander](https://github.com/wonderwhy-er/DesktopCommanderMCP), which
also supports web clients through remote MCP. The comparison below reflects its
official README as reviewed on 2026-07-11; competing projects evolve, so verify the
linked source rather than treating this table as permanent.

The differentiator is **security posture**, and it is real:

| | Desktop Commander | mcp-devbox |
|---|---|---|
| Default access | Broad filesystem and terminal capabilities | **Restrictive** (read-only default) |
| Secret-path/content denial | Not documented as a built-in invariant | **Built-in deny + output redaction** |
| Command access | General terminal with a configurable blocklist | **Closed allowlist, no free shell** |
| Directory jail covers commands | **No**; official README warns terminal commands can leave allowed directories | **Yes** |
| Write model | Direct writes and block edits | **Patch-first, validated before apply** |
| Consequential-action control | No equivalent planned single-use workflow documented | **TTL-bound plans, approval, revalidation, audit** |
| Repository handoff memory | No equivalent repository handoff contract documented | **Yes (`.agent-memory/`)** |

So the differentiator is not terminal access itself. It is the narrower authority
model: repository jail, secret denial, no free shell, planned consequential actions,
and agent-agnostic handoff memory. MCP Devbox still describes itself as
**secure-by-default, not secure** because universal OS isolation and egress control
remain unfinished.

## Platform

- **Core (Go): cross-platform.** Layer-1 policy works on Windows/Linux/macOS.
- **Hard isolation (Layer 2): Linux-only** (namespaces/seccomp/gVisor). On Windows,
  run under **WSL2** (real Linux kernel). Develop and run secure mode in WSL2.

## Licensing status

Copyright © 2026 Carlos Acosta. All rights reserved. This repository does not
contain an open-source `LICENSE`; public visibility does not grant permission to use,
copy, modify, distribute, sublicense, sell, or create derivative works. See
`COPYRIGHT` and `docs/open-source-release.md`. A future open-source or dual-license
release remains an explicit owner decision.

## Status

**Layer 1 (MVP) implemented.** Secure-by-default local MCP server in Go: read/search/
patch/test a local repo safely over the MCP stdio transport, with secret deny
(path + content), a path jail that also covers command execution, allowlist-only
commands, patch-first writes, approval gating, and an audit log. Tests (incl.
adversarial) + `go vet` + `gofmt` are green. See `docs/context-capsule.md`.

**v0.2 adds remote connectivity:** an HTTP transport (streamable-HTTP subset) with
**OAuth-first authentication with header-only bearer recovery**, designed to be exposed to ChatGPT web through a
self-hosted **Cloudflare Tunnel** (no inbound ports). Same Policy/redaction as stdio.

**Secure builder evolution:** the current P11.2 candidate exposes 78 deliberately annotated
tools, including rich repository status, narrow synchronization, planned GitHub
creation/remotes/publication, planned Coolify creation/deployment, persistent notes,
private validation profiles, bounded Coolify logs, and disabled-by-default
privileged profiles. Consequential operations use
cryptographically named, expiring, single-use plans and revalidate state before
execution. See [docs/tools.md](docs/tools.md).

The added `workspace_checkpoint` read-only primitive reconstructs branch, commit,
upstream, tree counts, diff statistics and a bounded redacted task summary in one
schema-only response. It does not fetch, modify Git, read source bodies or call an
external service.

The P11 candidate also persists content-free hourly/daily telemetry in embedded
SQLite and bounds operational JSONL: four 16 MiB observability segments and four
32 MiB audit segments under the existing `/state` volume. It stores no prompts,
parameters, results, private paths, identities, credentials or model reasoning.

Large tool output is redacted before persistence and replaced on the MCP boundary by
seven compact metadata fields plus an opaque `result_ref`. The bounded result store
uses `/state/results/results.db`, 24-hour success / 7-day failure TTLs, a 256 MiB
logical quota, exact search, and 16 KiB reads. It exposes no arbitrary paths,
embeddings, prompts, credentials, or model reasoning.

**P11.2 Edge/OpenCode candidate:** the VPS control plane can pair an independently
installed WSL device with a one-use, short-lived code and a per-device Ed25519 key.
The signed model-turn relay keeps the server, `mcp-edge`,
`model-turn-driver`, and OpenCode 1.18.1 as distinct processes. Only OpenCode enters
mandatory Bubblewrap; there is no direct-execution fallback. Docker proves the
relay without elevated capabilities, and an Ubuntu 22.04 host job proves network,
DNS, mount and private-path isolation. Installation and pairing remain explicit
human steps; see [the P11.2 baseline](docs/baselines/2026-07-16-p11_2.md) and the
[Parrot WSL guide](docs/install-opencode-edge-parrot.md).

**P12 Trusted Linux Workcell implementation candidate:** one explicit `linux-workcell`
profile adds default development behavior and an optional local `htb-linux`
context without changing the 85-tool MCP catalog. The existing `sandbox` remains
networkless; the trusted workcell honestly declares `trusted_host_shared_network`,
keeps authority and HTB metadata local to Edge, and permits only user-owned rootless
Docker or Podman sockets. It is not merged, deployed, paired, or installed on Parrot.
See [docs/linux-workcell.md](docs/linux-workcell.md).

**Architecture foundations:** P1 moved the complete public catalog into declarative
modules under `internal/mcpserver/catalog`. P2 split the implementation into focused
capability services over one shared policy, audit, root, runner, redaction, and
action-plan core. The public 62-tool wire contract remains unchanged while the
internal boundaries are easier to test and extend safely. P3 reduces
`cmd/mcp-devbox/main.go` to a true composition root and moves process orchestration
into focused modules under `internal/app`, while preserving every deployed command,
flag, environment variable and wire contract. P4 is deployed at
`4a68ca054a5f077d62a0f887234866673feb7353`; P5 deeper testing is deployed and
P6 CI/DevSecOps is deployed at `539e4d96c95aedd492ac36b428d4159054e183f4`
with the public 62-tool contract unchanged. Blocking CI, race, Staticcheck,
Govulncheck, CodeQL, Dependency Review, Docker/SBOM, and zero-High/Critical container
gates are proven in `docs/baselines/2026-07-13-p6.md`.

**P7 structured observability is deployed at
`d1309ed08db0170e5165f78bf406e94cfa56cc11`:** a separate closed-schema JSONL
stream for lifecycle, HTTP, JSON-RPC, malformed batches, and known public tool
completion without prompts, params, results, source, paths, targets, tokens,
identities, or raw errors. Race, Staticcheck, CodeQL, SBOM, and container gates are
green; production logs and the unchanged 62-tool catalog are recorded in
`docs/baselines/2026-07-13-p7.md`. It adds no public tool, endpoint, exporter,
or application; see `docs/observability.md`.

**P8 authenticated dark console is deployed at
`605a56d48a495f3c8a2ce62471223187ef2f5685`:** a dependency-free presentation
surface embedded in the existing Go HTTP application. PR and post-merge gates are
green; authenticated `cmd/console-smoke` verifies a Secure opaque session, exact
runtime schema, 62 tools, and the unchanged catalog hash. It cannot execute tools,
approve plans, list private resources, or read audit/observability history. No new
resident service, Coolify application, credential, listener, npm bundle, CDN, or OAuth
protocol change is introduced. See `docs/console.md` and
`docs/baselines/2026-07-13-p8.md`.

**P9 Brain is deployed and tagged `p9` at
`4fbe1dda02351c632e67c0f10a5c5b314df745e2`:** persistent Markdown truth,
local Git, disposable SQLite FTS5, `/brain`, 67 tools, `cmd/mcp-catalog-smoke` and `cmd/brain-smoke` are verified in production. The deterministic catalog hash is
`sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`.

**P8.1 Console 2.0 is deployed and tagged `p8.1` at
`d343264bffdc0ae1bc045a9d723e913be977090c`:** the existing
Go application now embeds a React/TypeScript/Vite Neo-BIOS operations firmware. It
shows only exact allowlisted real data, durable content-free task state and SSE; Brain
uses opaque graph IDs and Edge honestly remains Not paired. Console OAuth completes
server-side with PKCE/state/single-use codes and an opaque
`Secure; HttpOnly; SameSite=Strict` cookie. Query-string credentials always return
401, while an Authorization bearer remains recovery-only. No new MCP tool, listener,
application, resident service, free shell, autonomous agent or workcell is introduced.
Release-candidate evidence remains in `docs/baselines/2026-07-14-p8_1.md`; completed
remote gates, merge, deployment and production smokes are recorded separately in
`docs/baselines/2026-07-14-p8_1-production.md`.

Optional local Brain startup:

```bash
export MCP_DEVBOX_BRAIN_ROOT=/absolute/private/brain
go run ./cmd/mcp-devbox serve --root /abs/path/to/repo
```

Quick start:

```bash
go build ./...
# Local (stdio) — Cursor / Claude Desktop:
go run ./cmd/mcp-devbox serve --root /abs/path/to/repo            # read-only
go run ./cmd/mcp-devbox serve --root /abs/path/to/repo --mode ask --test-cmd "go test ./..."

# Remote (HTTP) — ChatGPT via Cloudflare Tunnel (bearer auth required):
export MCP_DEVBOX_TOKEN="$(openssl rand -base64 32)"
go run ./cmd/mcp-devbox serve --root /abs/path/to/repo --http :8765
cloudflared tunnel --url http://127.0.0.1:8765
```

**Connect from ChatGPT / Cursor / Claude Desktop:** see `docs/connect-remote.md`.

Complete OS isolation and egress control are not yet universal; the old cheap-model
worker plan is superseded. Honest scope and limitations: `SECURITY.md`.

## Secure builder workflows

```text
repo_list -> repo_status -> repo_fetch
-> repo_fast_forward_preview -> repo_fast_forward
-> source_repo_create_preview -> source_repo_create
-> repo_remote_preview -> repo_remote_set
-> repo_publish_preview -> repo_publish

platform_apps_list -> platform_app_create_preview -> platform_app_create
-> platform_deploy_preview -> platform_deploy -> platform_deployment_status
-> platform_app_status -> platform_app_logs
```

Use one tool call per message when that is more reliable for the ChatGPT connector.
This is workflow advice, not a security bypass. `git_commit` does not push; force
push and a free host terminal do not exist; tokens are never returned; external
writes require explicit approval in ask mode; aliases never weaken policy.

## How to build

1. `specify init` (GitHub Spec Kit) in this repo, integrated with your agent.
2. Feed Spec Kit's constitution from this repo's principles + `AGENTS.md`.
3. Read `docs/design.md` and `docs/security.md` before any code.
4. Build **Layer 1 (MVP)** first — already more secure than Desktop Commander.
5. Add Layers 2–3 (OS isolation, egress) only in v0.3, **wrapping** proven
   sandbox tech (gVisor/nsjail/Docker), never reinventing security primitives.

## Pointers

- `AGENTS.md` — operating rules for any AI working here (read first).
- `docs/design.md` — architecture, language/platform/tunnel decisions, MVP scope.
- `docs/security.md` — threat model + secure-by-default invariants (the product).
- `docs/context-capsule.md` — current state for resuming without re-hydrating chat.
- `docs/documentation-map.md` — documentation sources, status terms, and update rules.
- `docs/tools.md` — canonical tool, alias, annotation, workflow, and approval reference.
- `docs/testing.md` — reproducible race, fuzz, coverage, and integration gates with honest blocked/pass status.
- `docs/product-roadmap.md` — Cubethon delivery plus universal profiles, edge,
  orchestration, and authorized-security roadmap.
- `docs/security-engagements.md` — generic design for private, scope-bound,
  edge-enforced authorized security workspaces.
- `docs/edge-workcells.md` — flexible outbound WSL/Parrot workcells, local agents,
  privilege challenges, and infrastructure/security boundaries.
- `docs/linux-workcell.md` — exact P12 trusted-workcell architecture, local CLI,
  rootless containers, HTB context, residual risks, tests, and Parrot setup steps.
- `docs/open-source-release.md` — proposed public/private boundary, license options,
  and release-readiness checklist.


## Console durable live state candidate

The `console-durable-live-state` branch upgrades the presentation-only console without changing the 78-tool MCP catalog. It adds durable digest-only sessions, SQLite task/event history, recoverable SSE, stable safe Brain metadata, real opaque Project/Edge selectors and combined DB/WAL/log budget reporting. The release-candidate contract is documented in `docs/console.md` and `docs/baselines/2026-07-17-console-durable-live-state.md`. It is not merged or deployed.
