# Security policy

MCP Devbox reduces the authority available to AI-driven development workflows. It is
**secure-by-default, not secure**. The technical trust boundaries, threat model,
authority model, isolation profiles, persistence rules, limitations, and evidence are
maintained in [`docs/security.md`](docs/security.md).

## Supported versions

No formal long-term support matrix is currently published. Security fixes target the
current `main` line and the current production deployment unless the maintainer states
otherwise. Historical tags and files under `docs/baselines/` are evidence snapshots,
not promises of ongoing support.

A report should identify the affected commit, release, deployment identity from
`/version` or `system_runtime_info`, and—when relevant—the separately observed Edge
release. A source tag, VPS deployment, and installed Edge are different facts.

## Reporting a vulnerability

Report vulnerabilities privately to the repository owner or through GitHub's private
security-reporting channel when it is enabled. Do **not** open a public issue containing:

- an unpatched vulnerability or reliable exploitation steps;
- credentials, tokens, OAuth material, private keys, pairing codes, or secret values;
- private repository, Brain, audit, state, Edge, workspace, target, VPN, or host data;
- raw production logs or evidence that could identify another system.

When a private channel is not available, contact the owner before sending sensitive
details and provide only enough non-sensitive information to establish a secure route.

## What to include

A useful report contains:

- affected commit/release and observed runtime identity;
- affected architecture and profile: local stdio, HTTP control plane, Edge sandbox,
  trusted Linux workcell, authorized target-locked workspace, or development Edge Git;
- prerequisites and required policy mode;
- minimal reproducible steps using placeholders and a disposable repository;
- expected versus actual authority or isolation behavior;
- impact, including whether secrets, host paths, network access, publication, deployment,
  or approval boundaries are affected;
- relevant bounded/redacted diagnostics and suggested mitigation, if known.

Do not include a real secret to prove a leak. Use a synthetic canary.

## Scope

Security-sensitive areas include:

- filesystem jail, symlink and race handling;
- secret-path denial, content redaction, and local-human grants;
- command allowlists, patch-first writes, and approval policy;
- preview, single-use plans, expiry, replay prevention, and state revalidation;
- OAuth, header-only recovery bearer handling, console sessions, and public exposure;
- GitHub/Coolify owner, app, repository, branch, domain, and mount boundaries;
- result storage, audit, observability, OAuth persistence, and Brain isolation;
- the networkless Bubblewrap Edge sandbox;
- the `trusted_host_shared_network` workcell, which intentionally is not networkless;
- target/VPN revalidation for authorized target-locked workspaces;
- Ed25519-signed Edge releases, updater, rollback, repair, and local private state;
- the Development Edge Git boundary, where credentials stay outside the model workcell.

The landing page is presentation-only, but a bug that grants authority, weakens CSP or
authentication, exposes private data, or misrepresents a security boundary is in scope.

## Disclosure policy

Please allow reasonable time to investigate, reproduce, fix, test, deploy, and notify
before public disclosure. The maintainer will try to acknowledge the report, establish a
private coordination channel, classify impact, and communicate remediation status.
Exact timelines depend on severity, reproducibility, and operational constraints; none
are guaranteed by this document.

After a fix is available, coordinate publication so users can update first. Credit is
optional and follows the reporter's preference. Reports made in good faith against
systems and repositories the reporter is authorized to test are welcome. This policy
does not grant authorization to test third-party infrastructure or production data.

## Public discussion

Public issues may discuss hardening ideas, documentation errors, and already-remediated
problems without sensitive details. Never paste live environment variables, tokens,
private URLs, state files, repository content, targets, flags, or exploit chains into a
public issue.

## License status

The repository currently has no open-source `LICENSE`. Public visibility does not grant
permission to use, copy, modify, distribute, sublicense, sell, or create derivative
works. See `COPYRIGHT` and [`docs/open-source-release.md`](docs/open-source-release.md).
The security-reporting process does not change that status.
