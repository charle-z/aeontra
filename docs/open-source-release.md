# Open-source release boundary

Aeontra is licensed under the Apache License, Version 2.0 through the `mcp-devbox`
compatibility repository. `LICENSE`, `NOTICE`, `COPYRIGHT`, and
[`docs/provenance.md`](provenance.md) define the source-license and historical-author
boundary. Artifact-specific third-party notices remain a separate release gate.

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

- [x] Apache License 2.0, project attribution, and copyright posture are published in
      `LICENSE`, `NOTICE`, and `COPYRIGHT`.
- [x] Historical human and owner-directed automation identities are mapped in
      `docs/provenance.md`; future external contributions use the DCO.
- [ ] README comparison claims have dated primary sources.
- [x] Full working tree and complete Git history are scanned for secrets; exact
      synthetic redaction fixtures are narrowly documented in `.gitleaks.toml`.
- [x] `SECURITY.md` matches implemented isolation/egress limitations and does not imply
      that the source license authorizes security testing.
- [x] `CONTRIBUTING.md`, `SUPPORT.md`, governance, issue forms, and a concise pull-request
      template define the public contribution and support workflow.
- [ ] Non-blocking follow-up: add a standard code of conduct and a
      maintainer-approved private conduct-reporting channel without reusing the
      vulnerability inbox ambiguously. Their absence does not block licensing,
      publication, or leaving draft once the technical and provenance gates pass.
- [x] GitHub private vulnerability reporting is enabled and `SECURITY.md` defines the
      public reporting boundary.
- [x] CI runs tests, vet, formatting, dependency review, and secret scanning.
- [x] External Actions and versioned container bases are pinned by immutable commit or
      digest; dependency/license gates and container SBOM/checksum evidence are produced.
- [x] CI builds and initializes the local read-only stdio server through
      `scripts/verify-clean-install.sh` without private infrastructure values.
- [ ] Generate and review artifact-level third-party notice bundles from the exact
      release artifacts and their SBOMs.
- [ ] A clean Linux/WSL Edge installation, pairing, signed update, and rollback are
      reproduced by an independent operator without private maintainer defaults.
- [x] Fixed managed-deployment tools are moved behind an explicit maintainer profile or
      made owner-configurable; third-party installations do not expose this repository's
      Coolify application IDs, domains, or release repository as operative defaults.
- [x] The public console uses only synthetic/sanitized fixtures; repository tests reject
      maintainer usernames, private domains, and personal absolute paths in those fixtures.
- [ ] Signed releases and update paths are accepted on Linux/Parrot and Windows; live
      rollback acceptance remains pending on both device profiles.

## Positioning rule

Do not claim that Aeontra is categorically “secure” or that alternatives have no
security controls. State concrete, testable differences and current limitations.
The project documents a deny-by-default authority model with secret denial, command and
path jails, planned consequential actions, independent runner/Edge checks and auditable
handoffs. Positioning claims must point to those concrete controls and their limits.
