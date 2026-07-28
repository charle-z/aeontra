# Open-source release boundary

MCP Devbox is currently source-visible but not open source. Copyright © 2026
Carlos Acosta. All rights reserved. The `COPYRIGHT` file is the current legal notice.

Public visibility alone does not grant permission to use, copy, modify, distribute,
sublicense, sell, or create derivative works. Do not describe the project as open
source until the owner intentionally adds an open-source license.

## Future licensing options

The owner may later choose AGPL-3.0 plus a separate commercial license, a permissive
license, or another explicit arrangement. That decision remains deferred and should
consider contribution goals, hosted-service reuse, attribution, support obligations,
and commercial strategy.

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

- [x] Current copyright posture documented in `COPYRIGHT`; no open-source license granted.
- [ ] Owner chooses a future open-source, source-available, or dual-license model if desired.
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

