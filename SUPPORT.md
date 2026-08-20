# Support

Aeontra is community-maintained software. Support is best-effort and no service-level
agreement, response time, or compatibility promise is provided.

## Where to ask

- Use GitHub Issues for reproducible bugs and focused feature proposals.
- Use [`SECURITY.md`](SECURITY.md) and GitHub private vulnerability reporting for
  suspected security issues.
- Use the canonical documentation map in
  [`docs/documentation-map.md`](docs/documentation-map.md) before relying on an old
  baseline, chat transcript, or cached client tool list.

Do not use public issues for credentials, private URLs, OAuth material, production logs,
operator state, customer data, or unpatched vulnerability details.

## Supported state

Unless a release note says otherwise, fixes target the current default branch and the
latest supported release line. Source, server deployment, Front Door, and installed
Edge versions are independent facts. Include each identity that is relevant to the
problem.

Historical tags and `docs/baselines/` are evidence snapshots, not maintained support
branches.

## Useful bug reports

Include:

- the smallest reproducible example using a disposable repository;
- expected and actual behavior;
- source commit and `/version` or `system_runtime_info` identity;
- Edge release and host profile when relevant;
- the exact command or MCP operation category, without secrets;
- bounded and redacted logs;
- whether the issue reproduces on a clean checkout.

## Operational help

Maintainers cannot recover private keys, OAuth credentials, GitHub tokens, deleted
state, or third-party accounts. Do not grant maintainers ambient access to production
systems. Prefer documented diagnostics and disposable reproductions over interactive
access.

Commercial support, hosted service guarantees, enterprise support contracts, and
multi-tenant operations are not part of the current project scope.
