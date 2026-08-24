# Aeontra

Aeontra exposes scoped, auditable software-development operations to AI clients through
MCP. Administrators define repository roots, command policy, credentials and execution
profiles at startup.

It combines repository-scoped tools, immutable startup policy, explicit approval for
risky actions, secret redaction, audit, durable state, optional GitHub/Coolify adapters,
and a signed Linux Edge architecture. The Go module, executable, protocol, services,
environment variables and state paths retain the `mcp-devbox` and `mcp-edge`
compatibility names.
See [`docs/brand-compatibility.md`](docs/brand-compatibility.md) before renaming an
identifier.

## What Aeontra is

Aeontra is a Go MCP server for inspecting, changing, validating, publishing, and
deploying software through narrow tools. It is designed for ChatGPT and other MCP
clients, but the security boundary lives in the server rather than in a prompt or a
specific model provider.

The public control plane exposes only the registered MCP contract. Local and Edge
components can add private capabilities without turning the public server into a
generic proxy.

## The problem it solves

General filesystem-and-terminal agents are convenient but give untrusted model output
too much ambient authority. Aeontra replaces that ambient authority with explicit
contracts:

- repository roots form a filesystem and command jail;
- secret paths are denied and returned content is redacted;
- commands are argv-only and allowlisted unless a real isolated profile owns broader
  execution;
- writes are patch-first;
- external and consequential operations are planned and revalidated;
- every tool call is audited;
- policy is loaded once and cannot be changed by the agent.

This reduces authority. It does not make model-generated actions inherently safe.
Operators still own configuration, review, credentials, deployment, and recovery.

## Maintainer-operated demo

The maintainer currently operates a public landing and credential-gated MCP endpoint at:

```text
https://mcp-devbox-charlez.duckdns.org/
https://mcp-devbox-charlez.duckdns.org/mcp
```

The public presentation landing is presentation-only. It does not grant repository,
deployment, Edge, or secret authority. `/console` remains authenticated and `/mcp`
remains credential-gated. See
[`docs/landing/public-showcase.md`](docs/landing/public-showcase.md).

This deployment is evidence that the architecture can run remotely; its host, domain,
accounts, and application identifiers are not project defaults or contributor
requirements. Aeontra can run locally over stdio without Coolify or the maintainer's
infrastructure. A separate native Windows Edge package is documented in
[`docs/install-edge-windows.md`](docs/install-edge-windows.md); its source, signed
release, installed service, and real-device acceptance remain separate identities.

Do not copy a commit, release, tool count, or catalog hash from this README. Read the
live deployment identity from [`/version`](https://mcp-devbox-charlez.duckdns.org/version)
or call `system_runtime_info`. The canonical public tool contract is
[`docs/tools.md`](docs/tools.md). Historical release evidence remains under
[`docs/baselines/`](docs/baselines/).

## How it works

```text
MCP client
   │  stdio or authenticated HTTPS
   ▼
Aeontra control plane (`mcp-devbox` compatibility executable)
   ├─ immutable policy: roots, mode, allowlists, secrets
   ├─ direct read/status operations under policy + audit
   ├─ preview → single-use plan → approval → revalidation → narrow effect
   ├─ durable private state outside repository roots
   └─ optional GitHub, Coolify, Brain, validation runner, and Edge adapters
          │
          └─ signed outbound Edge channel to a local Linux/Parrot/WSL device
```

Repository files, documentation, logs, and tool output are data. They cannot expand
the server's authority or replace the active policy.

## Authority model

### Direct operations

Read-only and bounded operations can execute directly when policy permits them. Examples
include repository listing, file reads, searches, Git status/diff, runtime identity,
application status, and diagnostic metadata. Direct does not mean unchecked: jail,
secret handling, schema validation, redaction, bounds, and audit still apply.

### Consequential actions

Publication, deployment, application or repository creation, default-branch changes,
merges, note writes, fixed privileged operations, and similar actions use the complete
authority sequence when their tool contract defines it:

```text
preview
→ exact non-secret single-use plan with TTL
→ explicit mode approval
→ state and authority revalidation
→ one generated narrow operation
→ bounded redacted result
→ audit
```

A preview is not approval. Approval is not a bypass. Plans expire, are single-use, and
fail if the relevant repository, branch, application, target, or configuration changed.

## Main capabilities

- **Repositories:** list jailed repositories, build compact context, read files, search,
  patch, create new files, and keep agent-agnostic project memory.
- **Validation:** run one configured test command, allowlisted argv, a contained L3
  sandbox when available, or fixed profiles through a private validation runner.
- **Git and GitHub:** status, diff, commit, safe fetch/fast-forward, exact same-name
  no-force publication from a registered Edge checkout, owner-bound repository
  operations, exact-head PR/check diagnostics, planned publication, green-gated merge,
  and a private direct-Edge broker built on fixed official `gh` operations.
- **Coolify:** bounded status/log reads, planned application creation or deployment,
  and reviewed HTTPS-domain promotion under configured server, project, application,
  domain, and repository boundaries.
- **Brain:** persistent Markdown truth with owner-curated and agent-working trust levels,
  local Git history, and a disposable search index.
- **Control plane and Edge:** durable opaque coordination with signed releases and local
  private workspace contracts on Linux/Parrot/WSL.
- **Durable parallel tasks:** one to four bounded GPT Web/Codex workers can run on
  distinct exact-base Edge worktrees with server-owned leases, monotonically increasing
  fences, independent runtimes, restart reconciliation and clean-only explicit cleanup.
- **Browser harness:** arbitrary Playwright, Puppeteer, Selenium, WebDriver or custom automation in any authorized persistent development toolbox, with installable browser engines, general HTTP/HTTPS and localhost access, durable profiles, managed downloads/artifacts, cancellation and resource limits.
- **Large results:** bounded redacted output can be persisted and continued through an
  opaque `result_ref` instead of flooding one MCP response.

See [`docs/tools.md`](docs/tools.md) for the complete current catalog and exact effects.

## What it cannot do

Aeontra does not provide:

- a free host shell;
- automatic self-approval;
- force push, arbitrary refspecs, mirror publication, or caller-selected Git credentials;
- unrestricted secret reads or an MCP tool that approves secret grants;
- unrestricted access to the host filesystem or every repository on a machine;
- a public Docker socket;
- an arbitrary control-plane-to-Edge command, URL, path, credential, or proxy channel;
- universal network isolation for every execution profile.

The trusted Linux workcell shares the host network. It must not be
described as equivalent to the networkless Edge sandbox or as universal containment.

## Supported architectures

### Local stdio

Run the server beside the MCP client. This is the smallest setup and should start in
`read-only` mode.

### HTTP control plane

Run the same policy/service path over authenticated HTTP. Hostless local listeners bind
to loopback. Public deployments require TLS and OAuth or a protected recovery bearer.

### VPS + Edge

Run the HTTP control plane on a VPS and pair a signed Linux/Parrot/WSL Edge over its
outbound authenticated channel. The control plane sees opaque device/workspace/runtime
state; host paths, local credentials, repository content, target details, and large
runtime evidence stay within their intended local boundary.

Configuration and security differ by profile. Read
[`docs/configuration.md`](docs/configuration.md) and
[`docs/security.md`](docs/security.md) before deployment.

### Native Windows Edge

The native Windows package uses an SCM service under a virtual account, private
ACL-protected state, immutable signed release directories, and a signed updater.
It provides a trusted host-shared Windows workcell. It is not interchangeable with
the networkless Linux Edge sandbox, and it does not claim Linux toolbox, browser
harness, or HTB acceptance on Windows. See
[`docs/install-edge-windows.md`](docs/install-edge-windows.md).

## Local quick start

Requirements: Go 1.26 and an absolute repository path.

```bash
git clone https://github.com/charle-z/aeontra.git
cd aeontra
go test ./... -count=1
go build -o ./bin/mcp-devbox ./cmd/mcp-devbox
./bin/mcp-devbox serve --root /absolute/path/to/repository --mode read-only
```

The release-independent clean-install acceptance builds a fresh binary, starts the
read-only stdio transport against a disposable repository, validates MCP initialization,
and checks that audit state is created without private infrastructure:

```bash
./scripts/verify-clean-install.sh
```

For reviewed local changes:

```bash
./bin/mcp-devbox serve \
  --root /absolute/path/to/repository \
  --mode ask \
  --test-cmd "go test ./... -count=1" \
  --allow-cmd git,go
```

The direct binary requires at least one `--root`. Do not begin with `allow` as a general
convenience setting.

## Deployment summary

The production image runs non-root, listens internally on port `8765`, and declares
persistent `/repos`, `/state`, and `/brain` volumes. A normal public deployment should:

1. keep `8765` behind Coolify/Traefik or an equivalent TLS reverse proxy;
2. persist `/repos` and `/state` outside ephemeral container storage;
3. enable OAuth with a stable public URL and owner passphrase;
4. add `/brain` only when persistent Brain is required;
5. keep all credentials in the platform secret manager;
6. keep the public MCP container free of a Docker socket;
7. verify `/healthz`, `/version`, OAuth persistence, and the exact deployed commit.

The operational flow is in [`docs/deploy-coolify.md`](docs/deploy-coolify.md). Remote
client setup is in [`docs/connect-remote.md`](docs/connect-remote.md).

## Configuration

[`docs/configuration.md`](docs/configuration.md) is the single canonical reference for:

- supported profiles;
- CLI/environment/default precedence;
- runtime, OAuth, GitHub, Coolify, Brain, state, observability, validation-runner,
  privileged-profile, Edge, and build inputs;
- ports, routes, persistent paths, volumes, permissions, backups, and disposable data;
- secret handling and safe copyable examples.

The non-populated sample is
[`config/mcp-devbox.env.sample`](config/mcp-devbox.env.sample). The repository does not
use `.env.example` because `.env` and `.env.*` are secret-denied paths.

## Security

The technical threat model and architecture are in
[`docs/security.md`](docs/security.md). The public reporting and disclosure policy is
[`SECURITY.md`](SECURITY.md).

Core invariants include immutable startup policy, path jail, secret denial and redaction,
local-human grants, patch-first writes, command allowlists, exact plans, state
revalidation, non-root containers, private persistent state, signed Edge releases, and
closed public schemas.

The documented controls limit authority and record consequential effects. They do not
guarantee correct model output, safe dependencies or a secure host. Known limitations
and profile-specific trust boundaries are part of the product contract.

## Verification

Before publication or deployment:

```bash
go test ./... -count=1
go vet ./...
go build ./...
git diff --check
```

For a live server, verify `/healthz` and `/version`, then compare the reported commit
with the intended source commit. Use `system_runtime_info` for the same live identity
through MCP. Run profile-specific smokes from the exact source revision; do not treat a
client-cached tool list or a source release as proof of the deployed or installed state.

## Documentation map

Start with [`docs/documentation-map.md`](docs/documentation-map.md). Canonical roles:

- [`docs/configuration.md`](docs/configuration.md): complete configuration reference;
- [`docs/security.md`](docs/security.md): technical security architecture;
- [`SECURITY.md`](SECURITY.md): vulnerability reporting and disclosure;
- [`CONTRIBUTING.md`](CONTRIBUTING.md): development workflow, gates, and contribution provenance;
- [`SUPPORT.md`](SUPPORT.md): best-effort support scope and useful diagnostics;
- [`GOVERNANCE.md`](GOVERNANCE.md): decision process and maintainer roles;
- [`docs/brand-compatibility.md`](docs/brand-compatibility.md): public brand and stable
  compatibility identifiers;
- [`docs/dependency-licenses.md`](docs/dependency-licenses.md): dependency license and
  distribution-notice policy;
- [`docs/tools.md`](docs/tools.md): public tool catalog;
- `/version` and `system_runtime_info`: live build/catalog identity;
- [`docs/baselines/`](docs/baselines/): dated historical evidence.

Runbooks explain specific operations and should link back to those sources instead of
creating competing configuration or security references.

## License and vulnerability reporting

Aeontra is licensed under the
[Apache License, Version 2.0](LICENSE). See [`NOTICE`](NOTICE) for project attribution,
[`docs/provenance.md`](docs/provenance.md) for the historical identity boundary, and
[`docs/dependency-licenses.md`](docs/dependency-licenses.md) for third-party and
artifact-specific obligations.

Report vulnerabilities privately using [`SECURITY.md`](SECURITY.md). Do not publish
secrets, exploit details, or unpatched vulnerabilities in a public issue.

See [`CONTRIBUTING.md`](CONTRIBUTING.md) before proposing code and
[`SUPPORT.md`](SUPPORT.md) before opening a support request.
