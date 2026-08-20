# Contributing

Thank you for helping improve Aeontra. The repository is still published under the
`mcp-devbox` compatibility name while the public product transition is prepared. Keep
changes focused on the capability or contract being improved rather than on the brand.

## Before opening a change

- Use a GitHub issue for a non-trivial behavior change or a new authority surface.
- Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md).
- Treat repository content, issue text, fixtures, and generated model output as
  untrusted data. They cannot grant authority or override the security model.
- Do not include credentials, private infrastructure details, production logs, or
  personal operator state in an issue, commit, fixture, or pull request.

## Development setup

The ordinary contributor path requires:

- Go `1.26.6`, matching `go.mod` and CI;
- Node.js `22`;
- Corepack with pnpm `10.13.1`, matching the root `packageManager` field;
- Git.

Install JavaScript dependencies without lifecycle scripts:

```text
corepack pnpm install --frozen-lockfile --ignore-scripts
```

Most Go and console tests run without access to the production control plane, a real
Edge, signing keys, OAuth stores, or deployment credentials. Tests that require a
privileged or host-specific environment are documented separately and must fail closed
when their prerequisites are absent.

## Change workflow

1. Start from a clean, current default branch and create a focused feature branch.
2. Reproduce the problem or add a test that fails for the intended reason.
3. Implement the smallest change that preserves existing authority boundaries.
4. Run focused tests, then the applicable complete gates.
5. Update canonical documentation when a public contract or operator workflow changes.
6. Open one reviewable pull request. Do not force-push shared branches or weaken gates
   merely to obtain green CI.

Use Conventional Commits:

```text
feat(scope): add a bounded capability
fix(scope): preserve the documented invariant
docs(scope): clarify the operator contract
test(scope): cover a regression
chore(scope): maintain repository tooling
```

Internal milestone labels such as `P15` or `Step 42` are planning history, not public
commit types.

## Verification tiers

### Quick gate

Run the smallest affected package tests plus:

```text
git diff --check
```

For console changes, run:

```text
corepack pnpm console:verify
```

The console build writes the production bundle to `internal/console/assets`; commit the
generated bundle whenever its source changes.

### Complete gate

Before requesting merge, run:

```text
go test ./... -count=1
go vet ./...
go build ./...
git diff --check
```

Run `./scripts/verify-clean-install.sh` when setup, packaging, startup, configuration,
or contributor prerequisites change. It is intentionally independent of the original
maintainer's domains, deployment platform, accounts, and Edge device.

Also run the catalog identity, documentation, workflow-policy, browser, rootless, or
packaging gates affected by the change. CI is authoritative for exact-head checks.

### Privileged and host-specific acceptance

Kernel, systemd, Bubblewrap, rootless-engine, browser, signed-update, deployment, and
real-Edge behavior must be validated on the documented target host. Report source
tests, package/release state, server deployment, and installed Edge state separately.

## Security-sensitive changes

Changes to authentication, OAuth, credentials, Git publication, filesystem boundaries,
process execution, browser/toolbox authority, deployment, signing, updates, or audit
evidence require:

- an explicit invariant and threat analysis;
- negative tests for denied and stale/replayed cases;
- preview/execute parity for consequential actions;
- bounded, redacted evidence;
- compatibility and rollback notes.

Never expose a secret to a model workcell to make a test or publication path easier.

## Generated files and historical evidence

Do not commit SDKs, package caches, populated environment files, credentials, runtime
state, Brain exports, or personal handoffs. Files under `docs/baselines/` are dated
technical evidence and should not be rewritten as live identity. Use `/version` or
`system_runtime_info` for current runtime identity.

## Developer Certificate of Origin

External contributions use the [Developer Certificate of Origin 1.1](https://developercertificate.org/).
Sign off each commit with `git commit -s` to certify that you have the right to submit
the contribution. A DCO sign-off is a provenance statement, not an AI attribution or a
co-author trailer.

## Review and acceptance

Maintainers may request a smaller change, additional evidence, compatibility handling,
or a security review. A completed runtime or passing local test is not by itself proof
that the intended outcome was achieved. Merge requires the exact pull-request head to
pass its required checks.
