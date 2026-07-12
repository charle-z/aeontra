# Open-source release boundary

MCP Devbox benefits from an open-source core: its security claims become auditable,
contributors can test policy invariants, and the Cubethon demonstration is easier to
trust and reproduce. Public source is especially valuable for a tool that mediates
AI access to repositories and infrastructure.

Public visibility alone is not an open-source license. This repository currently has
no `LICENSE` file, so a license must be selected before describing the project as
open source.

## Recommended license

The recommended default is **Apache License 2.0** because it is permissive, widely
understood, and includes an explicit patent grant. MIT is simpler but has no equally
explicit patent language. AGPL-3.0 is appropriate only if requiring hosted modified
versions to publish their source is more important than broad adoption and simple
integration.

License selection is an owner/legal decision. Do not add one merely because an agent
recommended it.

## Public repository

Keep these public:

- Go control plane, policy, plan, redaction, OAuth, audit, and tool handlers;
- validation runner and generic administrator-owned profile contracts;
- generic edge/orchestrator protocols and provider interfaces;
- synthetic tests, threat model, deployment examples, and public console;
- generic engagement schema/design with no real program data;
- reproducible build, release, migration, backup, and rollback documentation.

## Private operator state

Never publish:

- `.env` files, Coolify/GitHub/provider tokens, OAuth stores, signing keys;
- `/state`, `/repos/.agent-memory`, production audit logs, or deployment backups;
- exact private-program assets, rules, credentials, cookies, sessions, evidence,
  hypotheses, reports, or program membership;
- personal PC/edge device identities, private network addresses, pairing material,
  or WireGuard/Tailscale configuration;
- real prompts/replays containing source, identities, targets, or customer data.

## Release blockers

- [ ] Owner chooses and adds `LICENSE`.
- [ ] README comparison claims have dated primary sources.
- [ ] Full working tree and complete Git history are scanned for secrets.
- [ ] `SECURITY.md` matches implemented isolation/egress limitations.
- [ ] `CONTRIBUTING.md`, code of conduct, support boundary, and private vulnerability
      reporting channel are added.
- [ ] CI runs tests, vet, formatting, dependency review, and secret scanning.
- [ ] Dependencies and container images are pinned; SBOM and checksums are produced.
- [ ] A clean clone can build and run without private infrastructure values.
- [ ] The public console uses only synthetic/sanitized fixtures.
- [ ] A tagged release and rollback procedure are tested.

## Positioning rule

Do not claim that MCP Devbox is categorically “secure” or that alternatives have no
security controls. State concrete, testable differences and current limitations.
The durable differentiator is a deny-by-default authority model with secret denial,
command/path jail, planned consequential actions, independent runner/edge checks,
and auditable handoffs—not simply access to a terminal from chat.

