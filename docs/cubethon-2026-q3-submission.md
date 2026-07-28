# Cubethon 2026 Q3 — public issue source

## MCP Devbox

MCP Devbox lets ChatGPT work on real projects and infrastructure without receiving a
free shell or unlimited authority. A general shell gives an agent ambient filesystem,
credential and command access far beyond most tasks; MCP Devbox replaces that with
narrow tools, closed schemas, repository and application boundaries, secret denial,
validation, redaction and audit. The owner chooses `read-only`, `ask` or `allow`:
inspection without effects, explicit review for policy-marked effects, or autonomy
inside authority configured in advance. Pixelgrama is the public end-to-end proof: it
was built, tested, published and deployed through MCP Devbox on CubePath.

MCP Devbox reduces the authority available to an agent. It does not make generated
code or every allowed operation inherently safe.

> Editorial status: this file is the canonical, versioned source for the public
> participation issue. Update the issue only after the landing is merged so its demo
> links and production claims point at the deployed commit.

## Evaluate it in under three minutes

1. **Guided read-only demo:**
   https://mcp-devbox-charlez.duckdns.org/#demo
2. **Pixelgrama in production:**
   https://pixelgrama.mcp-devbox-charlez.duckdns.org/wall
3. **Public source:**
   https://github.com/charle-z/mcp-devbox

The demo is public, unauthenticated and presentation-only. It cannot invoke MCP tools,
open the authenticated console, approve plans, request grants, access repositories or
read credentials. Its Pixelgrama history comes from the validated, versioned manifest
served by the same MCP Devbox binary.

## Why authority is different

A broad agent path looks like this:

```text
model
→ general shell
→ inherited credentials and environmental access
→ arbitrary commands
→ consequences that are difficult to bound
```

MCP Devbox instead uses:

```text
model
→ closed-schema tools
→ authorized repositories, branches, applications and targets
→ denied paths and secrets
→ validated commands and parameters
→ read-only / ask / allow
→ exact plans and state revalidation
→ bounded, redacted and audited result
```

### `read-only`

Inspects and diagnoses without writes or command execution. Repository jails, secret
denial, redaction, input/output limits and audit remain active.

### `ask`

Works normally, but effects marked as reviewable by policy wait for explicit approval.
This does not mean every read or safe step pauses.

### `allow`

Performs authorized work autonomously inside administrator-configured authority. It is
not a free shell: jails, secret protection, allowlists, schemas, validation, redaction,
audit, plans and revalidation remain active.

## Mode, plan and human grant are separate

```text
policy mode ≠ operation plan ≠ local human grant
```

A **mode** controls how authorized work proceeds. A **plan** binds one consequential
operation to an exact target and state, expires, is revalidated and can be consumed
once. In `ask` it can pause for approval; in `allow` that pause may be omitted when
the contract permits, but the plan remains exact and single-use. A **human grant** is
a separate local exception for sensitive paths and cannot be approved through an MCP
tool.

## Pixelgrama: public end-to-end evidence

Pixelgrama is a bilingual 16×16 pixel-art postcard wall with a closed data contract,
deterministic rendering, fixed versioned palettes, no accounts and no user tracking.
The guided demo links the public pull requests, successful checks, source commits,
production wall and `/version` identity.

The evidence distinguishes history from current production. Historical PR head SHAs
are not presented as the deployed commit. The canonical manifest currently records the
verified source-main and production commit as the same value on its stated verification
date. The exact historical `read-only` / `ask` / `allow` mode was not
published, so the demo says that directly instead of inventing it.

- Manifest: https://mcp-devbox-charlez.duckdns.org/showcase/pixelgrama-evidence.json
- Production identity: https://pixelgrama.mcp-devbox-charlez.duckdns.org/version
- Pixelgrama source: https://github.com/charle-z/pixelgrama

## CubePath is essential infrastructure

CubePath is not decorative hosting. The CubePath VPS runs the production MCP Devbox
control plane and persistent state, while Coolify builds and deploys the authorized
GitHub repository. OAuth, the authenticated console, Brain, bounded result storage,
validation coordination, deployment operations and the outbound Edge rendezvous all
participate through that deployment. Pixelgrama is also deployed through Coolify on
CubePath as the public end-to-end result.

Local workspaces and device secrets remain outside the public landing. Edge connects
outbound; visiting the landing grants no operational authority.

## Security controls

- No general shell in the public tool catalog.
- Repository and path jails cover reads, writes and command execution.
- Secret paths and returned content are checked independently and redacted.
- Commands come from closed allowlists with validated arguments and bounded output.
- Existing files use patch-first changes with validation before application.
- Consequential operations use exact, expiring, revalidated, single-use plans.
- GitHub operations are owner-bound; force push, mirror and arbitrary refspecs are absent.
- Coolify deployments are bound to an allowed application, repository, branch and commit.
- `/mcp` and `/console` remain authenticated; the landing is not a control plane.
- The public CSP allows only same-origin assets and the two public read-only resources.
- Audit and credentials remain private.

## Useful components

- **Control plane:** applies immutable startup policy, schemas, jails, redaction, plans,
  state and audit.
- **Brain:** persistent Markdown-backed memory with bounded search and reconstructible
  indexing; notes are data and cannot expand authority.
- **Repository memory:** `.agent-memory` preserves project-specific continuity and handoffs.
- **GitHub adapter:** repository, branch, PR, check, diagnostic and merge workflows under
  the configured owner.
- **Coolify adapter:** application status, bounded logs and commit-bound deployment plans.
- **Validation runner:** fixed validation profiles in a private contained environment,
  without exposing Docker or a free terminal to the public MCP process.
- **Edge:** outbound paired workcells for controlled Linux, Parrot or WSL projects.
- **Result references:** bounded, redacted retrieval of large results without exposing
  filesystem paths.
- **Authenticated console:** owner observability and operation, separate from the landing.

## Recommended captures — maximum four

1. Benefit-first landing hero and Pixelgrama proof.
2. Broad authority versus MCP Devbox, including the `read-only` / `ask` / `allow` selector.
3. Exact planned operation with its revalidation and bounded public result.
4. Pixelgrama wall together with its public deployment identity.

## Technical stack

- Go and Model Context Protocol.
- SQLite-backed durable state and Markdown-backed Brain.
- OAuth 2.0, PKCE and secure server-side sessions.
- Embedded HTML, CSS and JavaScript landing; React, TypeScript and Vite for the
  authenticated operations console.
- Bubblewrap and Linux namespace isolation for ordinary sandboxed workcells.
- systemd hardening and outbound paired Edge services.
- Rootless Podman or Docker only through explicitly authorized workcell profiles.
- GitHub Actions validation and security gates.
- Coolify on CubePath.
