# Historical feature framing

> **Historical / superseded product note.** This file preserves the early product
> framing and the decision not to build a separate cheap-model worker. It is not the
> current tool catalog, configuration reference, roadmap, or deployment status.

Use the current sources instead:

- [`../README.md`](../README.md) — current product entry point;
- [`configuration.md`](configuration.md) — supported profiles and configuration;
- [`security.md`](security.md) — technical security model;
- [`tools.md`](tools.md) — canonical public tool contract;
- [`product-roadmap.md`](product-roadmap.md) — current direction and evidence-based status;
- [`baselines/`](baselines/) — dated historical closure evidence.

## Historical decision

The original option-B plan proposed a separate low-cost model worker. That direction is
superseded. MCP Devbox is the policy-enforced tool and authority layer; the MCP client
owns the reasoning loop. Do not reintroduce `delegate_to_worker`, a generic autonomous
agent loop, or a second paid-provider dependency merely because an old phase document
mentions one.

## Durable feature principles

The feature set has expanded since the original Layer-1 inventory, so this file does not
list tools or counts. The durable principles are:

- read-only by default and reviewed writes through `ask`;
- repository jail for reads, writes, and command execution;
- secret-path denial plus content redaction;
- local-human grants for exceptional secret access;
- argv-only allowlisted commands, not a free shell;
- patch-first writes and explicit validation;
- repository content treated as untrusted data;
- bounded audit and observability;
- exact plans and revalidation for consequential effects;
- persistent handoff/memory that does not depend on one model vendor;
- profile-specific isolation rather than one universal sandbox claim.

The current implementation and aliases are defined in `tools.md`. Do not copy the
catalog into this file.

## Historical layered model

Early planning used the labels “Layer 1”, “Layer 2”, and “Layer 3” to separate
application policy, OS isolation, and egress control. That model remains useful as
history, but current security claims must use the actual surfaces in `security.md`:

- public control plane;
- Edge sandbox;
- trusted Linux workcell;
- authorized target-locked workspace;
- Development Edge Git broker;
- private validation runner.

The trusted workcell intentionally shares host networking. It must never be described as
networkless or as universal egress isolation.

## Definition of done for feature work

A feature is complete only when:

- its authority is narrower than the problem it solves;
- schemas, policy, redaction, audit, and failure behavior are explicit;
- focused and complete tests pass;
- exact-head CI is green;
- configuration and security documentation are updated in their canonical sources;
- historical evidence is added without rewriting prior baselines;
- deployment and real-device claims are verified separately when applicable.

Historical phases and discarded alternatives remain available in specs, ADRs, Git, and
`baselines/`. They are evidence, not active instructions.
