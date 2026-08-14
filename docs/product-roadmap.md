# MCP Devbox product roadmap

Last updated: 2026-08-14

## Direct GPT Web to Edge roadmap

The current product sequence keeps GPT Web as the reasoning client and MCP Devbox as
the durable execution authority. Direct execution does not create an OpenCode/model
runtime; OpenCode remains a tested optional fallback.

| Hito | Status | Current evidence / next gate |
|---|---|---|
| 1 direct vertical slice | Deployed | Direct signed control operations reached the paired Edge without a second model session. |
| 2 durable Edge operations | Deployed | Server-owned operation identity, leases, progress, cancellation and restart-safe queue state are in production. |
| 3 foreground execution | Deployed | `project_exec` uses arbitrary argv without an implicit shell in the trusted workcell. |
| 3A background start/status/stop | Real-device accepted on p15.0.18 | The real Parrot Edge passed idempotent start, incremental split output, repeated stop and explicit cleanup without OpenCode or a model runtime. |
| 3B recovery/list/signal/cleanup | Real-device accepted on p15.0.23 | A fresh process streamed incrementally, survived exactly one managed Edge restart and resumed at the next record without replay. Closed `interrupt` stopped the inner sandbox with known exit code 130 and no operator signal; repeated interrupt/stop were idempotent, exclusive cleanup succeeded, the final list was empty and doctor remained ready with `NRestarts=0`. |
| Safe Edge checkout synchronization | Real-device accepted on p15.0.20 | Status and authenticated fetch completed on the real checkout as a clean synchronized no-op: attached `main`, exact HEAD/upstream, ahead=0, behind=0 and no mutation. |
| 4 persistent universal toolbox | Real-device accepted on p15.0.20 | The real toolbox retained rootfs, marker, installed tools, rootless Podman socket and the same durable service across the coordinated Edge restart. Repeated service stop was idempotent and exclusive cleanup removed only that toolbox/service. |
| 5 split GitHub authority | Validation pending | Control-plane owner/public-OSS publication, PR, checks, merge and workflow operations are deployed. The Edge-private broker has accepted login/import, status and direct Git synchronization; a credential-free handoff from an Edge checkout to the control-plane publication path remains the missing acceptance. |
| 6 direct GPT Web parity | Validation pending | Foreground/background execution, persistent toolbox, rootless container access, managed browser harness, Git synchronization and durable results are deployed. One complete project delivery through only the direct path remains the acceptance gate. |
| 7 worktrees and deterministic parallelism | Planned | Durable isolated worktrees, ownership, fairness, recovery and explicit cleanup. |
| 8 full benchmark | Planned | Complete project delivery through the direct/Codex path with retained latency, resource, CI, browser and deployment evidence. |
| 9 durable task graph and multiagent | Planned; foundation implemented | The private P16 workqueue already provides durable jobs, dependencies, leases, fencing, fairness and backup. Public task/worktree/runtime association and multiagent acceptance remain unimplemented. |
| 10 Codex harness compatibility spike | Host accepted; productization pending | Stock Codex 0.147.0 reached a credential-free scripted Responses provider. Experimental App Server initialization also passed but is not a production dependency. No fork is justified; the signed adapter, package, real Edge acceptance and rollback release remain pending. |

## Accepted execution sequence — 2026-08-12

The next implementation order is deliberately narrower than the older milestone list:

1. Reconcile source, production, signed Edge release, live device state and continuation
   documents. Do not restart or update the Edge while operator-reported long-running
   work lacks a durable checkpoint.
2. Publish one stabilization release from an exact-head-green `main` and validate the
   real Edge independently from the backend deployment.
3. Run a Codex compatibility spike with a pinned official artifact and a scripted model
   provider. Prefer stock CLI/App Server; fork only after a recorded incompatibility.
4. Productize Codex as an optional signed Edge harness while retaining OpenCode as a
   rollback path for at least one accepted release.
5. Close the split GitHub handoff, then implement server-owned worktrees with one writer
   per worktree, exact base identities and explicit cleanup.
6. Connect the existing P16 workqueue to task, worktree, process/toolbox, model-turn and
   result identities. Begin with read-only and non-model parallel workers before
   allowing multiple writing agents.
7. Execute one full project benchmark and one public-OSS contribution benchmark before
   product-directory, multi-user or large-fleet work.

Automatic ChatGPT browser driving is not a durability primitive. The core continuation
contract is a content-bounded checkpoint plus a resumable task identity. Any later UI
automation must remain optional, use a manually authenticated browser profile and fail
without replaying consequential effects.

## Status snapshot — 2026-07-18

This table distinguishes implemented state from roadmap intent. Detailed acceptance
criteria below remain future requirements until their status changes with evidence.

| Surface | Status | Evidence / next boundary |
|---|---|---|
| P0-P5 architecture, hardening, and deeper testing | Deployed | `main` at `4a68ca054a5f077d62a0f887234866673feb7353`; production healthy; 62 tools. |
| P4 targeted L1 hardening | Deployed | `main` includes Steps 70-77; production verified. |
| P5 deeper testing | Deployed | `main` includes Steps 78-84; production verified; runtime catalog unchanged. |
| P6 CI/DevSecOps | Deployed | `main` and production serve `539e4d96c95aedd492ac36b428d4159054e183f4`; PR/push CI, CodeQL, Dependency Review, SBOM, and zero-High/Critical container gate are green. |
| P7 structured observability | Deployed | `main` and production serve `d1309ed08db0170e5165f78bf406e94cfa56cc11`; green Race/Staticcheck/CodeQL/container gates and real content-free JSONL logs are recorded in the P7 baseline. |
| Console/showcase | Deployed | P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259` is tagged `p8`; the original authenticated console remains historical evidence. |
| Brain memory | Deployed | P9 merge `4fbe1dda02351c632e67c0f10a5c5b314df745e2` is deployed and tagged `p9`; persistent `/brain`, 67 tools, catalog and Brain smokes are verified. |
| Console 2.0 / P8.1 | Deployed | `main` at `d343264bffdc0ae1bc045a9d723e913be977090c` is deployed healthy and tagged `p8.1`; React Neo-BIOS UI, console OAuth, strict query-key rejection, durable `/state/tasks`, SSE and exact safe-data contracts passed production smoke. |
| Public showcase | Implemented in source | The existing Go binary embeds the exact public `GET /` presentation-only landing; deployment is accepted only after all exact-head gates pass and the live commit matches the merge. |
| P11 bounded state and development Edge | Deployed / superseded by P11.2 and P12 | Historical 71-tool foundation remains in `docs/baselines/2026-07-15-p11.md`; the current production catalog has 85 tools. |
| P11.2 remote OpenCode relay | Deployed | The four-process relay, pinned OpenCode 1.18.1, restart/resume, Bubblewrap isolation, and zero-duplicate turn contract are deployed and form the P12 runtime foundation. |
| P12 Trusted Linux Workcell | Deployed and validated on Parrot | PR #25 merged at `3946fd7033f28906deb932298387034e2fa27fe8`; one real outbound Parrot runtime completed six sequences and an exact verified repository edit. Onboarding hardening packages `AF_NETLINK`, safe diagnostics, journal migration, and a reproducible preflight. |
| Asset broker | Not started | Requires explicit private/public asset and authority contracts. |
| Universal execution profiles | Planned | Registry/profile contract defined below; implementation not claimed. |
| Edge agents | Development and Trusted Linux Workcell validated | Outbound pairing, revocation, local STOP, Parrot WSL2, Bubblewrap, rootless Podman, and a real repository smoke are proven; broader security workcells and target-isolated networking remain planned. |
| Multi-model orchestrator | Planned | Must remain provider-neutral and subordinate to policy. |
| Authorized security workspaces | Planned | Requires administrator-owned active engagement scope. |

## Product definition

MCP Devbox is a universal, secure control plane for AI agents that need to work
with real repositories, build systems, infrastructure, and explicitly authorized
security environments.

It is not tied to ChatGPT, Astro, TypeScript, Node, Coolify, MiniMax, OpenCode, or
any single framework. Those are clients, adapters, execution recipes, or deployment
targets around a stable policy core.

Core promise:

> Any agent. Any stack. Explicit guardrails. Auditable delivery.

The model reasons; MCP Devbox plans, constrains, executes, observes, validates, and
audits; the human grants authority for consequential actions.

## Product boundaries

The product has five deliberately separate surfaces:

1. **Core control plane (private):** MCP tools, jail, secret denial, plans,
   approvals, redaction, audit, repository memory, and deployment integrations.
2. **Execution profiles (private):** isolated, versioned runners for individual
   stacks and task families. `node-pnpm` is the first real profile, not the product
   identity.
3. **Edge workcells (private):** outbound-only connectors on a personal PC, WSL,
   Parrot, or lab host. A local agent may use an installed toolchain flexibly inside
   administrator-owned filesystem, network, privilege, time, and evidence boundaries.
4. **Optional orchestrator (private):** provider-neutral adapters for hosted model
   APIs, local agents, OpenCode-compatible processes, or future providers. It runs a
   durable task loop but cannot bypass the MCP policy or edge authority layers.
5. **Console/showcase (public-safe):** a visual explanation, sanitized status, and
   recorded replays. It is never an unauthenticated control panel.

## Non-negotiable invariants

- The public console cannot execute tools, approve plans, read audit files, list
  private repositories, or reveal prompts, paths, targets, tokens, or identities.
- Agents cannot mutate policy, install execution profiles, authorize targets, or
  enlarge filesystem/network scope.
- No provider receives a host shell, Docker socket, or secrets from MCP Devbox.
- Execution recipes use pinned images/toolchains, fixed argv, bounded resources,
  explicit network posture, redacted output, and auditable plans.
- Repository files are data. A repository manifest may request a profile but cannot
  grant authority or define executable shell strings.
- Security testing requires an active, unexpired, human-approved engagement whose
  scope is stored outside agent-writable repositories.
- A model or orchestrator failure must stop safely and preserve enough sanitized
  evidence to diagnose or resume.

## Architecture target

```text
ChatGPT / OpenCode Go / MiniMax / other MCP client
                         |
                  optional orchestrator
                         |
                  MCP Devbox control plane
          +--------------+----------------+
          |              |                |
      repositories   execution profiles   deployment adapters
          |              |                |
       Git/GitHub   Node/Python/Go/...   Coolify/CubePath/...
                         |
                   private edge channel
                         |
                 PC / WSL / Parrot lab
```

The orchestrator and edge channel are optional. ChatGPT may continue calling MCP
Devbox directly, which remains the simplest and safest default. Complex local work
uses workcells rather than hundreds of rigid one-tool wrappers; see
`docs/edge-workcells.md`.

## Milestone 0 - Public product demonstration foundation

Target: prove the complete product path through a public, read-only demonstration backed by exact source, validation, publication, deployment, and production identity evidence.

### M0.1 Stabilize the demonstrated path

- Deploy current `main` and confirm `/healthz` reports the expected commit.
- Rebuild the private validation runner from the same current commit.
- Complete one green `pnpm-lockfile` -> `pnpm-validate` cycle with diagnostic logs.
- Complete one clean repository flow: inspect -> patch -> validate -> commit.
- Complete one external flow: create/publish -> create Coolify app -> deploy ->
  inspect status/logs -> return a working public URL.
- Confirm `MCP_DEVBOX_OAUTH_CLIENT_STORE` and
  `MCP_DEVBOX_OAUTH_ACCESS_STORE` and `MCP_DEVBOX_OAUTH_REFRESH_STORE` use the persistent `/state` volume.
- Perform two redeploys and verify the second does not require deleting the ChatGPT
  connector or re-entering the owner passphrase.

Acceptance:

- Every step has saved, secret-free evidence.
- Failure output identifies the failing command/stage.
- Working trees are clean after the demonstration.
- No secret appears in output, audit, Git remote URLs, screenshots, or video.

### M0.2 Build MCP Devbox Console

Ship the authenticated dark console first as embedded, dependency-free assets inside
the existing Go application. A separate unauthenticated public showcase may be built
later only when a public product presentation requires it; it must remain
presentation-only and must never proxy the private MCP control plane.

The current source implementation satisfies that later public-showcase boundary; see
`docs/landing/public-showcase.md` for the exact route, assets and acceptance contract.

Authenticated console scope:

- Product statement and clear problem/solution.
- Animated pipeline: Request -> Plan -> Patch -> Validate -> Commit -> Publish ->
  Deploy -> Live.
- Sanitized replay of a real run, including one failure and recovery.
- Interactive policy explorer using local simulation/data only: secret read denied,
  force push denied, expired plan denied, approved deployment allowed.
- Architecture map showing control plane, runner, GitHub, Coolify, and CubePath.
- Capability/profile view distinguishing implemented, experimental, and planned
  features.
- Safe live health indicator containing only public version/commit/availability.
- Visible `Hosted on CubePath` badge/footer and links to source, demo, security
  limitations, and creator profile.

The console must not proxy MCP calls or contain production credentials. A replay
must be clearly labeled as recorded/sanitized rather than live execution.

Acceptance:

- Authenticated HTTPS access works on desktop and mobile; unauthenticated users see
  only a minimal login surface.
- No authentication secret is present in frontend bundles, status payloads, logs, or
  JavaScript-readable browser storage.
- Accessibility basics, reduced motion, metadata, Open Graph, 404, and HTTPS work.
- The console stays useful when ChatGPT, GitHub, or Coolify is temporarily down.

### M0.3 Public release package

- Rewrite README positioning and correct the live tool count.
- Add a dated section listing the work completed during the initial public demonstration phase.
- Add an honest capabilities/limitations matrix and deployment architecture.
- Add setup, security, validation-runner, backup, upgrade, and rollback instructions.
- Record a 60-90 second primary demo and a longer technical walkthrough.
- Capture screenshots of console, ChatGPT plan/approval, validation, Coolify status,
  and the deployed artifact.
- Create a stable release/tag and freeze architecture before final smoke tests.
- Publish owner-approved evaluation material only outside the product repository and
  verify every public link from a clean browser session.

## Milestone 1 - Universal execution profiles

Goal: make language/framework support extensible without introducing a free shell.

### M1.1 Profile contract

Define an administrator-owned profile registry. Each profile contains:

- immutable id and version;
- pinned container image/digest and non-root uid/gid;
- detection hints that only suggest a profile;
- fixed lifecycle stages such as resolve, install, check, test, build, package;
- fixed argv templates with a closed set of validated parameters;
- read/write mount policy;
- resource limits and timeout;
- network policy per stage;
- expected artifacts and bounded/redacted output.

Profiles are installed by a local administrator. Agent-authored repository content
cannot add a profile, change an image, or provide a shell command.

### M1.2 Initial supported profiles

Implement in this order, driven by real use:

1. `node-pnpm` (existing profile, generalized under the contract).
2. `go-mod`.
3. `python-uv`.
4. `dockerfile-build` with controlled BuildKit isolation.
5. `static-site`.
6. `node-npm`, `python-poetry`, `rust-cargo`, and `java-gradle` as demand proves.

Frameworks such as Astro, React, Vue, FastAPI, Django, Laravel, or Spring remain
repository concerns detected within their language profile; MCP Devbox does not
hard-code itself around them.

### M1.3 Universal project workflow

Add planned tools that expose only profile selection and lifecycle stages:

```text
project_profile_detect
project_validation_preview
project_validation_execute
project_artifacts_list
```

`detect` returns evidence and candidates; it never executes. Execution stays
plan-bound, single-use, approval-gated, isolated, and audited.

Acceptance:

- At least Go, Python, Node, and Docker projects complete reproducible validation.
- A malicious repository cannot alter the selected runtime or escape the mount.
- Networked dependency resolution is separated from offline check/test/build.
- Every profile has adversarial tests and documented limitations.

## Milestone 2 - Private edge agents for PC, WSL, and Parrot

Goal: use MCP Devbox with machines outside the VPS without exposing inbound ports or
turning the VPS into an unrestricted remote shell, while still allowing a local agent
to combine the installed development, infrastructure, and authorized-security
toolchain inside a bounded workcell.

### M2.1 Edge transport

- Implement a small Go edge daemon or initially use an administrator-controlled
  WireGuard/Tailscale network while the native protocol is designed.
- Edge initiates the connection outbound; no public listener on the personal host.
- Pair devices with short-lived codes plus mutually authenticated long-term device
  credentials stored locally.
- Give every device its own identity, revocation state, allowed roots, workcell
  classes, network claims, concurrency, and expiration.
- Support provider-neutral local-agent adapters that receive a structured goal and
  bounded environment rather than infrastructure secrets.
- Keep local-human approval on the edge for sensitive reads or elevated actions.
- Heartbeats reveal only safe capability/status metadata.

### M2.2 Capability routing

The control plane may route a planned task to a named edge only when:

- the task profile is installed and allowed on that edge;
- the path and network scope are within its administrator policy;
- the plan is current and approved;
- the edge revalidates the same constraints independently.

The VPS must not be able to silently expand an edge policy. Revocation and an
emergency local stop must work without contacting the VPS.

Acceptance:

- Disconnecting the VPS or revoking the device prevents new work.
- Compromise of one edge credential does not authorize another device.
- No arbitrary host path, Docker socket, or private network is exposed. A workcell
  may permit a local shell to its contained agent, but the shell cannot enlarge the
  administrator-owned filesystem, network, privilege, duration, or engagement scope.
- All results are bounded, redacted, and tied to a task/plan id.

## Milestone 3 - Optional multi-model orchestrator

Goal: coordinate MiniMax, OpenCode Go, ChatGPT, or future providers while keeping
MCP Devbox as the sole authority and execution boundary.

### M3.1 Provider-neutral task protocol

Define a structured task state machine:

```text
requested -> planned -> awaiting_approval -> executing
          -> observing -> validating -> completed | failed | cancelled
```

Providers may propose goals, subtasks, patches, and tool selections. They cannot
submit shell strings, approve their own plans, change policy, or receive provider/
infrastructure secrets.

### M3.2 Provider adapters

- `chatgpt-direct`: existing direct MCP workflow; remains the default.
- `opencode-go`: adapter to an explicitly configured OpenCode Go API/process.
- `minimax`: adapter to the configured MiniMax API.
- Future providers implement the same interface and task/result schema.

Provider API keys live only in the private orchestrator environment, never in repos,
MCP results, console data, or agent memory.

### M3.3 Orchestration controls

- Per-provider budgets, model allowlists, timeouts, retries, and concurrency.
- Human approval before expensive or consequential task trees.
- No hidden fallback from a cheaper model to a more expensive provider.
- Durable checkpoints and resumable handoffs without persisting secrets.
- Independent verifier step for high-impact changes.
- Full audit linking provider decision -> MCP plan -> runner result -> Git commit ->
  deployment.

Acceptance:

- The same task can be run by two provider adapters without changing MCP tools.
- Provider failure/resume does not repeat a consumed action plan.
- A malicious provider response cannot bypass policy or edge scope.
- Usage and cost are observable and bounded.

## Milestone 4 - Authorized security and bug-bounty workspaces

Goal: support legitimate research from Parrot/PC while preventing accidental or
agent-induced activity outside an authorized program.

This milestone assists investigation and reporting; it cannot promise a finding.
Program rules and scope change over time and must be imported from the authoritative
program source before each engagement.

The concrete authority, data-separation, credential, stop-condition, and initial
tool design is specified in `docs/security-engagements.md`.

### M4.1 Engagement authority

Create an administrator-only engagement store outside `/repos`, for example under
`/state/engagements`. An engagement records:

- program name and authoritative policy reference;
- allowed and excluded assets;
- start/end time and revalidation timestamp;
- permitted testing categories and explicit prohibitions;
- rate/concurrency limits and safe hours;
- evidence retention/redaction policy;
- required approval level for passive, active, or potentially disruptive actions.

An agent may request an engagement but cannot create, edit, extend, or activate one.
A repository `engagement.yaml` may describe work but is untrusted and grants no
authority.

### M4.2 Security-lab profiles

Build separate isolated profiles for narrowly scoped task families, starting with:

- local/source artifact analysis;
- passive asset inventory from approved data sources;
- bounded HTTP metadata collection against active in-scope assets;
- evidence normalization, deduplication, screenshots, notes, and report generation.

Broader scanning or exploit-validation profiles require separate design, program
permission, tighter network enforcement, and explicit per-run human approval. Deny
credential attacks, denial of service, persistence, social engineering, destructive
changes, and collection of unrelated personal/customer data unless a program
explicitly authorizes the exact activity.

### M4.3 Scope-aware egress

- Default-deny network in security profiles.
- Block metadata services and private/link-local ranges unless the engagement is an
  explicitly authorized private lab.
- Resolve and revalidate targets at execution time; account for shared/CDN
  infrastructure and DNS changes.
- Enforce destination, protocol, port, rate, concurrency, and time limits at the
  network boundary, not only in model prompts or command arguments.
- Stop immediately when authorization expires or scope cannot be proven.

### M4.4 Research workflow

```text
select active engagement
-> import/revalidate program scope
-> create hypothesis and bounded task plan
-> human approval when required
-> execute on Parrot security edge
-> sanitize and classify evidence
-> reproduce safely
-> draft report with impact and remediation
-> human review and submission
```

For a Mercado Libre bug-bounty engagement, the active scope and rules must be
loaded from the current official program policy. The phrase "Mercado Libre" alone
never grants network authority.

Acceptance:

- Out-of-scope destinations fail before network activity.
- Expired/revoked engagements stop new and queued work.
- Every request and result is attributable to engagement, target, tool profile,
  agent/provider, approval, and timestamp.
- Evidence and reports redact secrets and unrelated personal data.
- Final submission always requires human review.

## Milestone 5 - Productization

- Versioned installation and migration paths for core, runner, profiles, console,
  orchestrator, and edges.
- Backup/restore and disaster-recovery tests for `/repos` and `/state`.
- Signed release artifacts and pinned base-image digests.
- Profile package integrity/signatures and an administrator-controlled registry.
- Safe diagnostics for OAuth, runner connectivity, profile availability, edge
  health, and deployment integrations.
- Metrics and tracing without prompts, source content, targets, or secrets.
- Security disclosure process, threat-model updates, and independent testing.
- Multi-user/team mode only after identity, tenant isolation, audit ownership, and
  authorization are designed explicitly.

## Prioritized implementation order

1. Finish and prove the end-to-end product path.
2. Ship the public-safe console and demonstration package.
3. Generalize the validation runner into universal execution profiles.
4. Add the private edge transport and one personal development edge.
5. Add provider-neutral orchestration, then OpenCode Go and MiniMax adapters.
6. Add administrator-owned engagements and a Parrot security edge.
7. Add narrow authorized-research profiles, expanding only with evidence and tests.
8. Productize installation, upgrades, profile distribution, diagnostics, and team
   use.

This order keeps the public demonstration focused while preserving the universal
architecture required for development, infrastructure, personal edge work,
multi-model orchestration, and authorized security research.
