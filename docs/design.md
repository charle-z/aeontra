# Historical design record

> **Historical / conceptual document.** This file preserves the early design rationale
> and durable architectural decisions. It is not the source for current deployment
> status, tool counts, releases, configuration, or installation instructions.

Current sources:

- [`../README.md`](../README.md) — product and supported architectures;
- [`configuration.md`](configuration.md) — canonical configuration and profiles;
- [`security.md`](security.md) — current trust boundaries and authority model;
- [`tools.md`](tools.md) — canonical public tool catalog;
- [`documentation-map.md`](documentation-map.md) — source ownership;
- [`adr/`](adr/) and [`baselines/`](baselines/) — decisions and dated evidence.

## Original problem

The project began from a narrow need: let an MCP client inspect and change selected
repositories without granting it unrestricted filesystem and terminal authority. The
useful distinction was never “works with every model”—that follows from MCP. The
product distinction is a deliberately narrower authority model with repository memory.

## Durable architecture

```text
MCP client
   │ stdio or authenticated HTTPS
   ▼
MCP Devbox control plane
   ├─ immutable startup policy
   ├─ repository jail and secret protection
   ├─ direct bounded operations under policy + audit
   ├─ planned consequential operations with revalidation
   ├─ durable private state outside repository roots
   └─ optional source, deployment, Brain, validation, and Edge adapters
          │
          ▼
      paired Edge profiles with local private contracts
```

The current tool surface is intentionally omitted. Read `tools.md` or live MCP
discovery. Read `/version` or `system_runtime_info` for live identity.

## Authority model

Read-only and bounded status operations may execute directly after schema, jail,
secret, redaction, and policy checks. Publication, deployment, creation, merge, and
other consequential actions use their documented sequence:

```text
preview
→ exact single-use plan
→ approval when required
→ state revalidation
→ narrow generated effect
→ bounded redacted result
→ audit
```

Preview is not approval. Approval cannot widen the plan. Compatibility aliases must use
the same handler and authority path.

## Durable decisions

### Go-first modular daemon

Go provides a cross-platform single binary and a straightforward implementation for
filesystem, Git, process, HTTP, and MCP contracts. Security comes from policy and
isolation, not from claiming that a language grants security. Keep the composition root
small and capability services focused.

### Repository-scoped memory

Structured handoff state belongs in repository-controlled Markdown, not only in chat or
a vendor-specific memory system. Persistent Brain is a separate administrator-enabled
profile with its own trust and storage boundary.

### Patch-first writes

Existing files change through validated patches. New-file creation refuses overwrite.
This keeps changes reviewable and avoids a generic full-file-write escape hatch.

### No free host shell

Layer-1 commands are argv-only and allowlisted. Broader execution exists only inside a
real, explicitly configured isolation profile or fixed privileged contract. The public
control plane must not become a generic terminal or Docker proxy.

### Profile-specific isolation

The old “Layer 1/2/3” model was useful planning shorthand, but current claims are made
per implemented surface:

- application-policy control plane;
- networkless Edge sandbox;
- trusted Linux workcell with host-shared networking;
- authorized target-locked workspace;
- Development Edge Git broker;
- private validation runner.

The trusted workcell is not universal egress isolation. Target-locking is not a host
firewall. Signed releases establish artifact identity, not correctness of the host.

### Outbound Edge control

The control plane sends closed operations and opaque identifiers over an authenticated
outbound channel. It must not accept arbitrary Edge commands, paths, URLs, scripts,
credentials, hashes, or targets. Local identity, workspace paths, credentials, and
sensitive evidence remain in local private state.

### Separate Git authority

A development Edge may use an owner-bound local Git broker. Credentials remain outside
the model workcell and are not exposed in schemas, argv, responses, or logs. Clone and
publication destinations are constructed from configured authority, not caller URLs.

### No separate cheap-model worker

The early low-cost delegated-worker proposal is superseded. MCP Devbox is the safe tool
and authority layer; the client owns the reasoning loop. A future unattended worker
must be justified as a separate, bounded product surface rather than smuggled into the
control plane.

## Historical alternatives

Early documents discussed a hosted relay, Cloudflare-only exposure, future versioned
layers, and a dashboard as undecided or future work. Those alternatives are historical.
Current transport, deployment, console, and Edge behavior is documented in the
corresponding runbooks and ADRs. Do not treat an old phase label as an active roadmap.

## Design acceptance

A design change is acceptable only when it:

1. states the authority required and authority deliberately unavailable;
2. identifies the trust boundary and honest isolation/network posture;
3. keeps credentials out of model-visible contracts;
4. defines failure, cancellation, replay, and recovery behavior;
5. has focused adversarial tests and exact-head CI evidence;
6. updates `configuration.md`, `security.md`, and `tools.md` only when their owned
   contracts actually change;
7. records historical closure in a new baseline instead of rewriting old evidence.
