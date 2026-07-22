# ADR 0004 — Global scheduler with separated VPS and Edge execution pools

Status: **Accepted for P16 implementation**

Date: 2026-07-22

## Context

MCP Devbox runs on a small production VPS that also hosts Coolify, Traefik, databases,
and applications. A measured no-cache Coolify build used nearly all two vCPUs for
part of its 133-second lifetime while RAM, storage, and monthly transfer retained
usable margin. Multiple chats can independently request builds, validations, and
deployments, so per-Dockerfile compiler limits cannot prevent aggregate saturation.

MCP Devbox also has an outbound Parrot/WSL Edge. The existing low-level workflow is
secure but has required opaque device/workspace/runtime IDs and repeated manual
installation, registration, and recovery operations. A chat or relay disconnect has
been capable of turning an otherwise valid setup into an unusable experience.

The system must continue supporting VPS builds as a development/production workflow;
GitHub Actions and image deployment are useful options, not mandatory replacements.
Future installations may contain multiple Edge devices with different capacities.

## Decision

Use one durable scheduler with separate execution pools for VPS and Edge devices.

P16 introduces one durable scheduler in the MCP Devbox control plane and separate
execution pools for the VPS and every Edge device.

```text
chats/agents
    -> MCP Devbox admission, policy, approval, durable queue
        -> vps.build / vps.deploy
        -> edge.<device>.build / edge.<device>.runtime
```

### Scheduler storage

Use a private SQLite database in `/state` for the first implementation.

Reasons:

- one control-plane replica and low/moderate job volume;
- transactional durability, uniqueness, leases, and recovery without another service;
- small resident-resource cost on a 4-GB VPS;
- existing operational experience with SQLite in MCP Devbox;
- same backup/restore boundary as other `/state` data;
- no new port, credential, network policy, Redis persistence mode, or separate backup.

The scheduler storage interface remains internal and replaceable. A future
multi-replica control plane may require a distributed database/queue, but P16 must fail
closed if deployed in an unsupported multi-writer topology.

### Execution separation

The public control-plane process never compiles. A separate VPS worker leases reviewed
jobs and invokes a separately constrained rootless build engine. Rootless BuildKit is
the first spike; rootless Podman is the fallback candidate if BuildKit fails a
structural requirement.

Each Edge retains independent identity, allowed roots, profiles, credentials, resource
maxima, and local enforcement. The control plane cannot enlarge those values.

### Immutable target

Every approved job is bound to either the VPS or one exact Edge. No automatic fallback
or migration exists. Changing target requires a new plan, job, and approval.

### Fairness and resource policy

Use hard multidimensional admission plus Deficit Round Robin by project/workspace.
Deduplicate identical work atomically. Run adaptive/priority equations in shadow mode
until measured data justifies ordering changes.

Initial VPS policy is one active heavy build and an isolated CPU calibration over
50/65/80 percent of one vCPU. Cgroups/rootless controls enforce policy; environment
variables only configure bounded administrator values.

### Edge lifecycle and user interface

Treat installation simplicity and durable project aliases as part of the scheduler
architecture, not separate polish.

- clean installation uses the signed Debian package plus one guided onboarding action;
- valid P12/P15 identity and state migrate without re-pairing;
- signed updates are atomic and roll back on failed migration/health;
- normal chat flows identify projects and targets by human aliases;
- internal `ed_*`, `ws_*`, `mr_*`, job, lease, and idempotency values are hidden from
  normal interaction;
- workspaces are safely discovered, associated, or cloned under approved roots;
- jobs and pending results survive chat/control-plane/Edge reconnect boundaries.

### Platform and image deployment

VPS Dockerfile builds remain supported through the limited worker. P16 later adds an
optional build-to-image and deploy-by-digest flow for VPS or Edge builds. Git,
registry-push, and Coolify registry-pull credentials remain separate.

## Alternatives considered

### Keep direct Coolify builds and only optimize Dockerfiles

Rejected as the sole solution. Individual compiler concurrency limits do not constrain
Node, Docker/BuildKit, package extraction, multiple projects, or simultaneous chats.
They also provide no queue, fairness, durability, deduplication, or Edge continuity.

### Move every build to GitHub Actions

Rejected as a requirement. It reduces VPS load but gives up the desired private
VPS/Edge development workflow, introduces external CI dependency, and does not solve
project/workspace/Edge lifecycle. It remains an optional executor/source of images.

### One global pool for VPS and Edge

Rejected. It incorrectly couples independent machines and trust domains. An Edge build
must not consume VPS capacity or inherit VPS authority, and vice versa.

### Automatic target selection/fallback

Rejected. It creates surprising cost, trust, path, credential, and data-location
changes. Explicit target binding is safer and easier to audit.

### Redis queue

Rejected for P16. Redis is fast, but another resident service, memory reservation,
credential, persistence policy, backup, upgrade, and network boundary are unjustified
for one control plane and modest job volume. Reconsider only with evidence of SQLite
contention or a supported multi-replica architecture.

### In-memory mutex/semaphore

Rejected. It loses state on redeploy, cannot reconcile external effects, does not
coordinate Edge devices, and cannot provide durable fairness/deduplication.

### Worker inside the MCP Devbox container

Rejected. A build failure or resource spike would share the public control-plane
failure domain, and child builder processes might escape the intended container limit.

### Require users to keep managing opaque IDs

Rejected as a product failure. Opaque IDs remain diagnostic/audit identifiers; durable
alias resolution is the normal interface.

## Consequences

Positive:

- predictable host behavior under many chats;
- durable queue, fairness, cancellation, deduplication, and recovery;
- independent Edge capacities and no silent trust-domain migration;
- VPS builds remain available without monopolizing production;
- installation/update/reconnect experience becomes usable rather than procedural;
- future image deployments can remove VPS compilation without forcing GitHub Actions.

Costs:

- one additional private worker and rootless builder on the VPS;
- new scheduler/project/journal schemas and migration responsibility;
- more state-machine and external-reconciliation testing;
- builds may take longer under CPU limits;
- offline continuation requires careful bounded local authority;
- scheduler-managed Coolify applications require bypass detection and break-glass docs.

## Validation

This decision is validated only after:

- rootless builder spike and 50/65/80 quota measurements;
- one-active-build proof under 20 concurrent requests;
- zero observed control-plane 502 during calibration;
- clean and migrated Edge installation without re-pairing;
- project-by-alias operation from a new chat;
- disconnect/reconnect without duplicate build or result;
- per-Edge capacity tests with no target fallback;
- exact-head CI, production smoke, rollback evidence, dated baseline, and `p16` tag.
