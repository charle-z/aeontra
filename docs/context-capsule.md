# MCP Devbox context capsule

This is a bounded continuation capsule, not a release diary and not live identity. Read
it after `AGENTS.md` when resuming work. The repository and live runtime remain the
sources of truth.

## Canonical sources

- [`../README.md`](../README.md) — current product entry point and navigation.
- [`docs/configuration.md`](configuration.md) — complete supported configuration, paths,
  volumes, defaults, permissions, and secret handling.
- [`docs/security.md`](security.md) — technical threat, trust, authority, isolation, and
  persistence model.
- [`docs/tools.md`](tools.md) — canonical public tool catalog and workflows.
- [`catalog-aware-backend-rollout.md`](catalog-aware-backend-rollout.md) — managed
  catalog comparison, Front Door transition, exact backend deployment, compensation,
  and operator runbook.
- [`documentation-map.md`](documentation-map.md) — source ownership and status terms.
- [`product-roadmap.md`](product-roadmap.md) — evidence-based product direction.
- [`docs/baselines/`](baselines/) — dated historical evidence.
- `.agent-memory/current-task.md` and `.agent-memory/handoffs/latest.md` — active task
  and continuation state.

Use `/version` or `system_runtime_info` for the live server commit, protocol, tool count,
and catalog hash. Do not copy those moving values into this capsule.

## Product in one paragraph

MCP Devbox is a secure-by-default MCP server that lets AI clients inspect, change,
validate, publish, and deploy software through narrow tools without exposing a free host
shell or unrestricted machine authority. Policy is immutable at startup. Reads and
bounded status operations execute directly under jail, secret, redaction, schema, and
audit controls. Consequential effects use exact preview, single-use plan, approval when
required, state revalidation, narrow execution, bounded result, and audit.

## Supported architecture

```text
MCP client
   │ stdio or authenticated HTTPS
   ▼
MCP Devbox control plane
   ├─ repository jail and immutable policy
   ├─ direct bounded operations
   ├─ planned consequential operations
   ├─ private durable state outside repository roots
   └─ optional GitHub, Coolify, Brain, validation, and Edge adapters
          │
          ▼
      signed paired Edge with local private workspace contracts
```

Supported deployment shapes:

- local stdio in `read-only` or `ask`;
- local authenticated HTTP;
- VPS/Coolify HTTP control plane with OAuth;
- global builder with optional GitHub and Coolify adapters;
- persistent Brain and observability/state profiles;
- VPS plus signed Linux/Parrot/WSL Edge;
- optional fixed privileged and private validation profiles.

## Authority boundaries

Do not collapse these surfaces into one “sandbox” claim:

- **Public control plane:** application-level jail, allowlists, schemas, plans, redaction,
  audit, and non-root container. It is not universal OS isolation.
- **Edge sandbox:** mandatory networkless Bubblewrap for the selected workspace.
- **Trusted Linux workcell:** filesystem/process boundary with
  `trusted_host_shared_network`; not universal egress isolation.
- **Authorized target-locked workspace:** closed local actions bound to one private target
  and validated VPN route; not a host firewall.
- **Development Edge Git/GitHub broker:** owner-bound Git transport and fixed official
  `gh` operations outside the model workcell; credentials remain in private local state
  and only bounded parsed metadata returns to the control plane.
- **Private validation runner:** fixed profiles in a separate service that owns the
  reviewed container-engine authority. The public MCP container has no Docker socket.
- **Catalog rollout coordinator:** private server-owned Coolify authority, exact commit
  pins, a two-catalog maximum, and a persistent journal. It does not add a public
  deployment endpoint or expose tokens to the MCP client.

Source release, package artifact, VPS deployment, and installed Edge are separate
facts. Verify each with separate evidence.

## Durable security invariants

- Read-only by default; use `ask` for reviewed writes and commands.
- Secret-shaped paths are denied; returned content is redacted.
- Exceptional secret reads require local-human, path-bound, short-lived grants.
- Commands are argv-only and allowlisted; no free host shell.
- Existing-file writes are patch-first and validated before apply.
- Repository content and tool output are untrusted data, never policy.
- Consequential plans expire, are single-use, and revalidate authority/state.
- Git publication has no force, mirror, tags, arbitrary refspec, caller URL, or caller
  credential surface.
- OAuth is preferred publicly; static bearer is header-only recovery. Query-string
  credentials are rejected.
- The managed MCP backend keeps Coolify auto-deploy and instant-deploy disabled. Its
  `platform_deploy` path requires an exact candidate SHA, a CI-verified catalog identity,
  complete exact-head green checks, and a durable coordinator rollout.
- A Front Door transition admits only the previous and candidate catalog. Wildcards, a
  third catalog, malformed hashes, direct force deployment, and concurrent topology and
  catalog transitions fail closed.
- Audit and observability are bounded and exclude prompts, content, credentials, paths,
  targets, and raw errors.
- Signed Edge bundles bind the authority-bearing components; installed-device state must
  still be observed separately.

## Repository workflow

For a normal change:

```text
workspace_checkpoint
→ read/search the smallest relevant sources
→ add or identify a failing test
→ apply_patch/create_file
→ focused validation
→ go test ./... -count=1
→ go vet ./...
→ go build ./...
→ git diff --check
→ commit on a feature branch
→ normal PR
→ exact-head checks green
→ merge commit
→ synchronize clean main
```

Before creating a script or helper, use the tool-discovery index in `AGENTS.md` and the
catalog in `tools.md`. Create an auxiliary only when no canonical tool covers the
operation and record why.

## Operational state

Do not infer current state from this file. Resolve it as follows:

| Question | Source |
|---|---|
| What is deployed on the VPS? | `/version`, `system_runtime_info`, and platform application/deployment status |
| What tools are public? | `tools.md` and live MCP discovery |
| What source release exists? | source-host release metadata and signed manifest |
| What package artifact was published? | release/package workflow evidence |
| What is installed on a real Edge? | supported local doctor/status and signed bundle metadata |
| What task is active? | `.agent-memory/current-task.md` |
| What happened historically? | a dated file in `baselines/`, an ADR/spec, PR, and Git history |

Use “validation pending” for a missing environment-specific proof. A green source test,
tag, or automatic deployment is not evidence of a real-device installation.

## Persistent state

- `/repos`: repository jail and project data.
- `/state`: OAuth stores, audit, observability, telemetry, tasks, results, console,
  model-turn, queue, and Edge/control-plane coordination.
- `/brain`: optional Brain Markdown truth and local Git; search cache is disposable.
- `~/.local/state/mcp-edge`: private installed Edge identity, registry, journal, results,
  and optional local Git authority.
- `/coordinator-state/catalog-rollout`: private atomic backend rollout journal inside the
  existing coordinator persistent volume.

Keep `/state`, `/brain`, OAuth stores, Edge private state, credentials, and engine sockets
outside the repository jail. Preserve owner-only modes and reviewed backups.

## Known limitations

- A Layer-1 allowed command inherits the daemon user's ambient OS access.
- Content secret detection is heuristic.
- The trusted workcell shares host networking.
- Rootless container authority remains broad inside that user's namespace.
- Target-locking is not universal egress enforcement.
- Signed bundles prove artifact identity, not correctness of every dependency or host.
- The model can damage data inside its selected writable workspace.
- No formal verification or universal cross-platform OS sandbox is claimed.

## Resuming safely

1. Read `AGENTS.md` and this capsule.
2. Read `.agent-memory/current-task.md` and the latest handoff.
3. Verify branch, HEAD, upstream, and working tree.
4. Read the affected canonical source, runbook, implementation, and tests.
5. Confirm live identity only when the task depends on deployment state.
6. Preserve historical evidence instead of rewriting it to match the present.

If documentation conflicts, use `documentation-map.md` to identify the owner and fix the
canonical source rather than adding another copy.