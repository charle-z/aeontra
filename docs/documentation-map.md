# Documentation map

This file defines which source answers each class of question. The goal is one canonical
answer per topic, with runbooks linking to it instead of copying mutable tables or live
state.

## Canonical product sources

| Source | Canonical role | Must not become |
|---|---|---|
| `README.md` | Short product introduction, architecture overview, safe quick start, and navigation | a phase diary, full configuration table, tool catalog, or threat model |
| `docs/public-alpha.md` | One bounded local evaluation path, first acceptance, and feedback boundary | a second configuration reference, hosted-service promise, or live deployment report |
| `docs/install-edge-linux.md` | Short signed Linux/Parrot/WSL Edge installation and verification path | a release baseline, package internals reference, or private maintainer setup |
| `docs/configuration.md` | The only canonical inventory of supported profiles, flags, environment variables, build inputs, ports, routes, paths, volumes, permissions, defaults, and secret handling | a deployment-status report or historical baseline |
| `docs/security.md` | Technical security architecture: trust boundaries, threat model, authority model, profile isolation, persistence, audit, limitations, and evidence | the public vulnerability inbox or a duplicate configuration reference |
| `SECURITY.md` | Public reporting, scope, supported-version posture, disclosure, and license status | a copy of the full technical threat model |
| `CONTRIBUTING.md` | Contributor setup, change discipline, verification tiers, provenance, and review expectations | an internal milestone plan or operator handoff |
| `SUPPORT.md` | Best-effort support boundary, supported-state language, and useful diagnostic inputs | an SLA or private troubleshooting channel |
| `GOVERNANCE.md` | Public decision model, roles, and maintainer path | enterprise governance or private security-response procedure |
| `LICENSE`, `NOTICE`, and `COPYRIGHT` | Source license, project attribution, and copyright notice | a dependency inventory or artifact-specific notice bundle |
| `docs/provenance.md` | Historical human/automation identity mapping and future DCO boundary | a replacement for Git history or legal advice |
| `docs/brand-compatibility.md` | Public product name, compatibility identifiers, and rename boundary | a mass-replacement checklist or deployment identity source |
| `docs/dependency-licenses.md` | Reviewed dependency-license classes and distribution-notice requirements | legal clearance for project source or a release SBOM |
| `CHANGELOG.md` | User-visible release changes | a full Git history or mutable live deployment identity |
| `docs/versioning.md` | Public version format, release identities, process and retention | permission to delete an artifact or rewrite a Git tag without review |
| `docs/tools.md` | Canonical public MCP tool catalog, schemas, annotations, aliases, approval posture, and workflows | a hardcoded live deployment claim |
| `/version` and `system_runtime_info` | Live server version, commit, protocol, tool count, and catalog hash | documentation to be copied into operational prose |
| `docs/baselines/` | Dated historical evidence, including exact commits, releases, hashes, counts, checks, deployments, and real-host observations | current operational instructions rewritten to match later state |

When these sources disagree, fix the source that owns the topic. Do not create a second
canonical table in a runbook.

## Other authoritative project sources

- `.specify/memory/constitution.md`: durable engineering and security principles.
- `AGENTS.md`: concise operating rules for agents working in this repository.
- `.agent-memory/`: optional operator-local task and handoff state; it must remain untracked
  and cannot override repository or live-runtime evidence.
- Brain: optional durable server-side continuation notes. Brain is operational state,
  not a substitute for versioned product contracts or Git history.
- `specs/001-layer-1/` and later `specs/`: accepted requirements, plans, and task
  history for their specific scope.
- `docs/context-capsule.md`: bounded project context and evidence pointers for resuming
  work; it may contain dated state but is not the configuration or live identity source.
- `docs/product-roadmap.md`: product direction and evidence-based status, not setup.
- `docs/runbooks/`: workflow-specific operations and recovery.
- `docs/adr/`: architectural decisions and their rationale.
- Git history and pull requests: exact change provenance.

## Status vocabulary

Use status only when evidence supports it:

- **Deployed:** merged, exact deployment identity verified, required health/smoke checks
  passed, and the result is recorded.
- **In progress:** active implementation exists but one or more required gates remain.
- **Planned:** accepted scope exists; implementation has not started.
- **Not started:** no accepted implementation work exists.
- **Validation pending:** implementation exists, but a named environment-specific,
  exact-head, package, deployment, or real-host proof is still missing.
- **Historical:** retained to explain a past design or release and not an active
  operational instruction.
- **Superseded:** replaced by a named source or design; retained only for context.

Never infer “deployed” from a branch, local test, tag, source release, or documentation
claim alone. Source/release, VPS deployment, and real Edge installation must be reported
as separate facts.

## Runbook contract

A runbook owns one workflow. It may state the variables and paths unique to that flow,
but must link to `docs/configuration.md` for the complete configuration inventory and to
`docs/security.md` for the technical model.

Every operational runbook should cover, where applicable:

> setup, configuration, permissions, validation, update, rollback, and troubleshooting

Runbooks may include exact application IDs, branches, commits, or releases only when
those values are intrinsic to a dated closure/evidence procedure. General installation
and operations must use live lookup or placeholders instead of embedding mutable state.

## Configuration ownership

`docs/configuration.md` owns:

- supported local, HTTP, VPS, builder, Brain, observability, Edge, privileged, and
  validation-runner profiles;
- CLI/environment/default precedence;
- every administrator-controlled runtime variable and build input;
- `/repos`, `/state`, `/brain`, OAuth, result, audit, telemetry, console, model-turn,
  Edge, and local Edge state paths;
- ports, volumes, ownership, modes, persistence, backup, and disposable data;
- secret classification and safe examples.

A flow guide such as `docs/connect-remote.md` or `docs/deploy-coolify.md` should list only
its minimum required variables, then link to the canonical reference. It must not carry
a competing “complete” environment table.

## Security ownership

`docs/security.md` owns technical claims about:

- application policy versus OS isolation;
- direct operations versus consequential planned actions;
- OAuth, recovery bearer, console, and public exposure;
- secret grants and redaction;
- GitHub/Coolify/validation boundaries;
- networkless Edge sandbox, trusted host-shared workcell, target-locked workspace, and
  Development Edge Git broker;
- signed releases, persistence, audit, observability, and known limitations.

`SECURITY.md` owns how a reporter contacts the project and coordinates disclosure. The
README should summarize and link, not reproduce either source.

## Tool and live identity ownership

- Use `docs/tools.md` when a human needs the public tool contract.
- Use MCP discovery or the server catalog when a client needs callable schemas.
- Use `/version` or `system_runtime_info` for the live deployment identity.
- Use a dated file under `docs/baselines/` when discussing historical tool counts,
  hashes, commits, checks, releases, deployments, or device evidence.

Operational documents must not hardcode a current number of tools, current catalog hash,
current deployed commit, or current device release. Those values change independently.

## Historical evidence

Files under `docs/baselines/` are immutable historical evidence except for narrowly
justified factual corrections. A later release must not rewrite an earlier baseline to
look current. If later evidence supersedes a snapshot, add a new baseline or a clearly
linked current document.

Historical phase plans may remain in specs, ADRs, roadmap entries, and Git. They must be
marked Historical or Superseded when a reader could mistake them for active setup or
current product behavior.

## Evidence and specialized documentation index

These references remain useful, but they do not replace the canonical product sources
above:

- Public product site contract: `docs/landing/public-site.md`.
- Public alpha local evaluation and feedback path: `docs/public-alpha.md`.
- Signed Linux/Parrot/WSL installation entry point: `docs/install-edge-linux.md`.
- Stable independently deployed MCP facade: `docs/stable-mcp-front-door.md`.
- Frozen historical Pixelgrama presentation snapshot: `docs/showcase/pixelgrama-evidence.json`.
- GitHub Actions diagnosis and bounded log retrieval: `docs/github-actions-diagnostics.md`.
- P8 closure evidence: `docs/baselines/2026-07-13-p8.md`.
- P8.1 production closure: `docs/baselines/2026-07-14-p8_1-production.md`.
- P9 release-candidate evidence: `docs/baselines/2026-07-14-p9.md`.
- P11 historical candidate evidence: `docs/baselines/2026-07-15-p11.md`.
- P11.2 historical relay evidence: `docs/baselines/2026-07-16-p11_2.md`.
- P15 historical release-candidate evidence remains under
  `docs/baselines/2026-07-19-p15-rc.md`; P14 target-locked runtime actions are
  documented in `docs/authorized-htb-actions.md`.
- P16 scheduler specification: `specs/007-global-work-scheduler/`.
- P16 pool architecture decision:
  `docs/adr/0004-p16-global-scheduler-separated-execution-pools.md`.
- L3 and native Windows execution boundary decision:
  `docs/adr/0005-separated-l3-and-native-windows-execution.md`.
- Private rootless L3 deployment and acceptance:
  `docs/runbooks/private-sandbox-runner.md`.
- P16 measured capacity evidence: `docs/baselines/2026-07-22-p16-capacity.md`.
- Pre-Codex source/production/Edge reconciliation:
  `docs/baselines/2026-08-12-operational-reconciliation.md`.
- Stock Codex CLI/App Server compatibility decision:
  `docs/analysis/codex-harness-compatibility.md`.
- Edge lifecycle state migration: `docs/edge-lifecycle-migration.md`.
- P16 package/install candidate: `docs/install-edge-parrot-p16.md`.
- Public signed Linux/Parrot/WSL install path: `docs/install-edge-linux.md`.
- Native Windows Edge package and operator boundary: `docs/install-edge-windows.md`.
- Dual Linux/Parrot and native Windows `v1.2.24` operational acceptance:
  `docs/baselines/2026-08-27-v1_2_24-dual-edge.md`.
- Signed `v1.2.25` release, notice assets and device updates:
  `docs/baselines/2026-08-27-v1_2_25-release.md`.
- Human project aliases and workspace resolution:
  `docs/project-workspace-resolution.md`.
- Durable local Edge execution journal: `docs/edge-job-journal.md`.
- Durable control-plane queue store: `docs/workqueue-store.md`.

Exact commits, releases, hashes, counts, job IDs, deployment observations, and device
proofs belong in those dated evidence files or in live identity output. Source/release,
VPS deployment and real Edge installation must be reported as separate facts.

## Change checklist

Before merging a documentation change:

1. Identify the canonical owner for every changed fact.
2. Verify configuration claims against code, Dockerfiles, entrypoints, workflows, and
   the live runtime where relevant.
3. Verify security claims against actual policy and profile implementation.
4. Replace mutable counts, hashes, releases, and commits with canonical/live references
   unless the document is dated evidence.
5. Keep historical baselines intact.
6. Update focused documentation contracts.
7. Run `go test ./... -count=1`, `go vet ./...`, `go build ./...`, and
   `git diff --check`.
8. Record exact-head CI and merge evidence before claiming completion.
