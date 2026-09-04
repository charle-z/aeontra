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
Operator-local `.agent-memory/` files and optional Brain notes may carry continuation
state, but they remain untracked instance data and are not canonical product documents.

Use `/version` or `system_runtime_info` for the live server commit, protocol, tool count,
and catalog hash. Do not copy those moving values into this capsule.

## Product in one paragraph

MCP Devbox is the Aeontra compatibility server. It lets AI clients inspect, change,
validate, publish and deploy software through a registered MCP tool catalog. Policy is
immutable at startup. Reads and bounded status operations execute under jail, secret,
redaction, schema and audit controls. Consequential effects use exact preview,
single-use plans, approval when required, state revalidation, bounded execution and
audit.

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

The development-environment v2 contract keeps four roots separate for every registered
workspace: the source checkout, private runtime state, private package/compiler cache,
and private managed artifacts. On Linux these are derived below the Edge state root as
`project-runtime/<workspace-id>`, `project-cache/<workspace-id>`, and
`project-artifacts/<workspace-id>`; Windows derives the same layout from its configured
state root without fixing a drive letter. Toolchain homes and package-manager caches
are pointed at these roots rather than being created in the Git tree.

Project resolution is registry-first. A dirty tree is an authorized development state;
`unavailable`, `timeout`, `identity_mismatch`, `corrupt`, and `unsafe_boundary` describe
different failures. Process status and termination use the durable identity captured at
process start, so they do not depend on a clean tree or a second Git discovery pass.
The Edge scheduler uses bounded shared capacity for ordinary independent operations and
an exclusive gate for signed update, rollback, and repair. Toolbox and project registry
records have versioned identity/generation metadata and additive migrations; recovery is
explicit and never resets the source checkout.

Supported deployments include local stdio or authenticated HTTP, a VPS/Coolify control
plane with OAuth and optional GitHub/Coolify adapters, persistent Brain and state, and
signed Linux, Parrot, WSL, or Windows Edge devices. Privileged validation remains an
optional fixed private profile.

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

The source release, package artifact, VPS deployment, and installed Edge are separate facts. Verify each with separate evidence.

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

For a normal change: checkpoint; read the smallest relevant sources; establish RED;
implement and refactor; run focused and complete tests, vet, build, and diff checks;
commit on a feature branch; open a normal PR; require exact-head green checks; merge;
then synchronize clean main.

Before creating a script or helper, use the tool-discovery index in `AGENTS.md` and the
catalog in `tools.md`. Create an auxiliary only when no canonical tool covers the
operation and record why.

## Operational state

Do not infer current state from this file. Use `/version`, `system_runtime_info`, and
platform status for the VPS; `tools.md` plus live discovery for the public tools;
source-host metadata and signed manifests for releases; workflow evidence for packages;
local doctor/status plus bundle metadata for an installed Edge; current Git state plus
operator-local `.agent-memory/` or Brain for active work; and `baselines/`, ADRs, PRs,
and Git history for historical evidence.

Use “validation pending” for a missing environment-specific proof. A green source test,
tag, or automatic deployment is not evidence of a real-device installation.

For development-environment v2, also verify these separately: source checkout state,
workspace attestation, private runtime/cache/artifact roots, toolbox generation and
rootless mount identity, scheduler capacity, and durable process binding. A dirty source
tree is not by itself evidence of an unsafe boundary. A process that has already been
authorized is observed and stopped using its captured binding, even if the project
registry is later released or the source tree changes.

## Persistent state

`/repos` holds jailed projects; `/state` holds OAuth, audit, telemetry, tasks, results,
model turns, queues, and coordination; `/brain` holds optional Markdown truth.
`~/.local/state/mcp-edge` holds private Edge identity, registries, journals, local Git
authority, managed worktrees, and the per-workspace `project-runtime`, `project-cache`,
and `project-artifacts` roots. `/state/workqueue` contains durable groups, leases and
fences; `/coordinator-state/catalog-rollout` contains the rollout journal.

Keep `/state`, `/brain`, OAuth stores, Edge private state, credentials, and engine sockets outside the repository jail. Preserve owner-only modes and reviewed backups.

## Known limitations

Layer-1 commands inherit the daemon user's access; content secret detection is
heuristic; trusted workcells share host networking; rootless authority is broad within
its user namespace; and target-locking is not universal egress control. Signed bundles
prove artifact identity, not host correctness. A model can damage its selected writable
workspace. Edge capacity is bounded: ordinary operations overlap, but signed mutations
remain exclusive and a full pool may wait until deadline. No formal verification or
universal cross-platform OS sandbox is claimed.

## Resuming safely

Read `AGENTS.md`, this capsule, and the current handoff or Brain checkpoint; verify
branch, HEAD, upstream, and working tree; then read the affected canonical sources and
tests. Confirm live identity only when deployment matters, and preserve historical
evidence rather than rewriting it.

If documentation conflicts, use `documentation-map.md` to identify the owner and fix the canonical source rather than adding another copy.
