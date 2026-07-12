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
**mandatory bearer auth**, designed to be exposed to ChatGPT web through a
self-hosted **Cloudflare Tunnel** (no inbound ports). Same Policy/redaction as stdio.

**Secure builder evolution:** the server now exposes 59 deliberately annotated
tools, including rich repository status, narrow synchronization, planned GitHub
creation/remotes/publication, planned Coolify creation/deployment, persistent notes,
private validation profiles, bounded Coolify logs, and disabled-by-default
privileged profiles. Consequential operations use
cryptographically named, expiring, single-use plans and revalidate state before
execution. See [docs/tools.md](docs/tools.md).

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
- `docs/tools.md` — canonical tool, alias, annotation, workflow, and approval reference.
- `docs/product-roadmap.md` — Cubethon delivery plus universal profiles, edge,
  orchestration, and authorized-security roadmap.
- `docs/security-engagements.md` — generic design for private, scope-bound,
  edge-enforced authorized security workspaces.
- `docs/edge-workcells.md` — flexible outbound WSL/Parrot workcells, local agents,
  privilege challenges, and infrastructure/security boundaries.
- `docs/open-source-release.md` — proposed public/private boundary, license options,
  and release-readiness checklist.
