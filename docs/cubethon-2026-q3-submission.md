# Cubethon 2026 Q3 — submission draft

Status: draft ready for owner review. Official submission deadline: 2026-07-26.
Do not submit until the final screenshots, Discord user, and evaluator access path
are confirmed.

## Project name

MCP Devbox — Secure remote development workcells for AI agents

## Description

MCP Devbox is a secure-by-default MCP control plane that lets ChatGPT and other AI
agents work on real repositories without receiving a free host shell or unrestricted
filesystem access. It combines repository jails, secret denial and redaction,
patch-first writes, bounded commands, approval-bound consequential actions,
persistent agent-agnostic memory, an authenticated operations console, and an
outbound Parrot WSL Edge.

The Trusted Linux Workcell runs pinned OpenCode inside Bubblewrap, selects only a
locally registered workspace, keeps device identity and host paths off the VPS, and
optionally exposes a verified user-owned rootless Podman or Docker socket. A real
remote smoke completed six model-turn sequences and made an exact verified Git
workspace edit.

## Demo URL

`https://mcp-devbox-charlez.duckdns.org`

## Public repository

`https://github.com/charle-z/mcp-devbox`

## Technologies

- Go
- Model Context Protocol (MCP)
- SQLite
- OAuth 2.0, PKCE, and secure server-side sessions
- Server-Sent Events
- Bubblewrap and Linux namespaces
- systemd hardening
- OpenCode 1.18.1 with a private Unix-socket model-turn driver
- Podman/Docker rootless
- React, TypeScript, and Vite for the embedded operations console
- Coolify on CubePath

## How CubePath is used

CubePath hosts the production MCP Devbox control plane and authenticated console.
Coolify deploys the public GitHub repository on the CubePath VPS. The Parrot Edge
connects outbound to that CubePath-hosted service; no inbound port is opened on the
user machine. Production state, OAuth, the console, the MCP transport, runtime
leases, and safe operational metadata live in the CubePath deployment, while local
workspace contents and device secrets remain on Parrot.

## Screenshots/GIFs still required

1. authenticated Neo-BIOS console overview;
2. Brain graph and tool catalog;
3. Edge/workspace state before and after pairing;
4. completed Parrot smoke showing the six-sequence runtime and exact file result;
5. optional short GIF from remote goal to verified repository change.

## Final registration checklist

- [x] Public repository.
- [x] Production deployment is healthy on CubePath.
- [x] README visibly includes a Hosted on CubePath badge and production URL.
- [x] README documents the project and its security posture.
- [x] Real outbound Parrot workcell smoke completed.
- [ ] Confirm a judge-accessible demo path that does not reveal owner credentials.
- [ ] Capture and attach screenshots/GIFs.
- [ ] Insert the exact Discord username used in `#cubethon`.
- [ ] Re-run production health and smoke checks from the final submission commit.
- [ ] Create the Cubethon issue before the official deadline.

## Suggested issue body values

**Name:** MCP Devbox — Secure remote development workcells for AI agents

**Description:** Use the description above, shortened only if the issue form has a
practical limit. Keep the claims about shared host networking and rootless socket
authority honest.

**Demo:** `https://mcp-devbox-charlez.duckdns.org`

**Repository:** `https://github.com/charle-z/mcp-devbox`

**CubePath usage:** Use the CubePath section above.

**Discord:** `<ADD_DISCORD_USERNAME>`
