# Technical security model

MCP Devbox is **secure-by-default, not secure**. It narrows the authority exposed to an
AI client through immutable startup configuration, closed schemas, repository jails,
secret denial and redaction, explicit approval, single-use plans, revalidation, audit,
and profile-specific isolation. These controls reduce risk; they do not prove that
model-generated code or every permitted process is safe.

The public reporting and disclosure policy is [`../SECURITY.md`](../SECURITY.md).
Configuration requirements are canonical in
[`configuration.md`](configuration.md).

## Trust boundaries

The system has several distinct authorities. They must not be collapsed into one vague
“sandbox” claim.

| Surface | Authority | Isolation | Network | Secret handling |
|---|---|---|---|---|
| Public control plane | Registered MCP tools, configured repository roots, optional GitHub/Coolify/Brain adapters, durable server state | Application-level jail, schemas, allowlists, plans, non-root container; not a universal OS sandbox | VPS/container network according to deployment | Secrets come from administrator configuration, are omitted from tool schemas, and are redacted from output/audit |
| Edge sandbox | One registered local workspace and private runtime files | Mandatory Bubblewrap with no direct-execution fallback; selected workspace writable, host/private paths excluded | Networkless | Edge identity, provider credentials, and local state stay outside the namespace |
| Trusted Linux workcell | One registered owner-controlled development workspace; optional rootless engine | Bubblewrap filesystem/process boundary plus explicit local contract; not equivalent to the networkless sandbox | `trusted_host_shared_network` | Only reviewed local authorities are brokered; host secrets and rootful Docker remain excluded |
| Authorized target-locked workspace | Trusted workcell plus one locally registered private target, VPN interface, and authorization revision | Same workcell boundary plus closed target actions and session handles | Host-shared network; actions revalidate exact target and VPN route, not general egress filtering | Credential values and sensitive saved output remain local; the control plane receives opaque handles/metadata |
| Development Edge Git broker | Owner-bound clone and planned publication for a registered `dev` workcell | Git transport runs outside the model namespace through a closed local broker | Only constructed owner-bound GitHub transport | Credential is stored in private `0600` Edge state, passed only to a fixed askpass child, and never enters workspace, argv, model schemas, or logs |
| Public OSS GitHub broker | Public external issue/PR reads plus planned fork, comment and cross-repository PR writes | Public control-plane schemas, fixed API routes, owner/upstream validation, expiring single-use plans and exact state revalidation | GitHub API for one named public upstream and the configured owner's fork | Reuses the server-side `GITHUB_TOKEN`; it never enters the Edge toolbox, repository, argv, tool schema, output or audit |
| Direct Edge checkout sync | Registered project checkout and fixed `origin` only | Read status/fetch plus exact single-use fast-forward plans; dirty, detached, ahead, diverged, stale or replayed state fails closed | Existing owner-bound GitHub transport only | Reuses the private askpass authority; public results omit credential, URL, path, argv and PID |
| Direct Edge GitHub broker | Repository already bound to a registered development project | Only server-constructed official `gh api` argv execute outside the workcell; no arbitrary endpoint, header, command or caller repository is accepted | GitHub API for the exact owner/repository only | `GH_TOKEN` exists only in the bounded child process environment under a private HOME; results are parsed, bounded and token-redacted before safe capability metadata leaves the Edge |
| Managed browser harness | Any authorized `dev` workcell and its persistent rootless toolbox, project workspace, installed browser/tooling rootfs, managed run trees and named persistent profiles | Arbitrary caller argv runs inside the existing workcell/toolbox boundary; no new host terminal, Windows mount, host home, rootful socket or external Edge state is added. The validated user-owned rootless engine remains the workcell authority already documented for toolbox use | General workcell networking: ordinary HTTP/HTTPS Internet, private development endpoints and localhost services are available. MCP Devbox does not impose a browser-domain/action/JavaScript allowlist | Caller code, cookies, authentication stores, downloads and artifacts stay on the Edge. Public tools return only opaque lifecycle, bounded redacted logs, relative artifact metadata and exact bounded chunks; argv, environment, PID, container identity, profile content and host paths remain private |

Additional boundaries:

- The MCP client and model are untrusted callers.
- Repository files, prompts, documentation, logs, and tool output are data, never
  authority or policy instructions.
- The administrator controls roots, mode, authentication, integrations, volumes,
  signing trust, and local Edge installation.
- GitHub, Coolify, OAuth, and the private validation runner are separate external trust
  domains with independently scoped credentials.
- Source release, VPS deployment, and installed Edge state require separate evidence.

## Threat model

MCP Devbox assumes an AI client may emit malicious, mistaken, or prompt-injected tool
calls. It also assumes a repository can contain hostile filenames, symlinks, patches,
hooks, configuration, documentation, build scripts, and output designed to escape its
intended scope.

Primary threats include:

- traversal, symlink, TOCTOU, and path-confusion escapes;
- secret discovery or exfiltration through reads, errors, logs, Git, environment, or
  external integrations;
- shell injection, option injection, destructive commands, hooks, credential helpers,
  or caller-controlled transport;
- approval confusion, stale previews, plan replay, state changes after review, or
  aliases that bypass policy;
- arbitrary GitHub/Coolify owner, repository, branch, domain, application, mount, or
  deployment selection;
- public authentication bypass, OAuth state/replay errors, query-string credential
  leakage, or unsafe console sessions;
- oversized results, unbounded logs, persistent sensitive state, or cross-tenant
  correlation;
- control-plane commands expanding into arbitrary Edge paths, targets, credentials,
  URLs, hashes, or scripts;
- unsigned, partial, mixed, downgraded, or incorrectly installed Edge components;
- overclaiming a trusted workcell or target contract as universal network isolation.

Out of scope as guarantees: correctness of generated code, safety of all permitted
third-party dependencies, a formal proof of isolation, and protection from a fully
compromised administrator or host kernel.

## Authority model

Policy is loaded once at startup. The agent cannot add roots, switch mode, expand an
allowlist, enable privileged profiles, set credentials, approve grants, replace signing
trust, or mutate the catalog at runtime.

Every tool has one exact schema and a known effect. Compatibility aliases, where they
exist, share the same handler, validation, approval posture, and audit path. Tool output
cannot create authority for a later call.

The three policy modes are:

- `read-only`: no writes or command execution;
- `ask`: potentially consequential effects require explicit approval;
- `allow`: allowlisted direct effects may proceed without an approval prompt; reserved
  for trusted local automation and not a general production recommendation.

Mode does not bypass jail, secret protection, schema validation, plan binding,
revalidation, redaction, or audit.

## Direct operations

Bounded reads and status operations execute directly when policy allows them. Examples
include repository listing, file reads, searches, Git status/diff, runtime identity,
application status, health, and catalog discovery.

Direct execution still enforces:

- canonical roots and path containment;
- traversal, symlink, special-file, and secret-path checks;
- bounded inputs and output;
- content redaction;
- no shell interpretation;
- known external destination and authentication rules;
- audit and closed observability.

Allowlisted local commands are argv arrays, not shell strings. The default command list
is conservative. A permitted Layer-1 process still runs as the daemon user, so the
application policy is not an OS sandbox and cannot stop that process from reading all
resources available to the same account.

## Consequential actions and plans

Consequential effects use the complete flow when their tool contract defines it:

```text
preview
→ exact non-secret single-use plan with TTL
→ explicit approval when mode requires it
→ repository/configuration/remote state revalidation
→ one generated narrow operation
→ bounded redacted result
→ audit
```

Plans use server-generated opaque identifiers, bind all security-relevant state, expire,
are consumed once, and reject replay. Approval applies only to the reviewed plan; it
cannot change its target or arguments. If branch HEAD, upstream, remote, application,
configuration, target, or another bound value changes, execution fails and a new preview
is required.

Examples include publication, merge, default-branch change, repository/application
creation, deployment, notes, fixed privileged tasks, and managed validation-runner
creation. There is no force push, mirror, tags, caller refspec, caller credential, free
host command, arbitrary Coolify payload, or arbitrary Edge operation.

## Secret handling and grants

Defense is layered:

1. Secret-shaped paths such as `.env`, `.env.*`, SSH material, credentials, keys, and
   private state are denied before a normal read.
2. Returned text passes through content redaction.
3. Tool schemas do not accept integration credentials, signing keys, pairing secrets,
   OAuth stores, or local Edge credentials.
4. Tokens are sent only to their intended service and are never returned in output.
5. Audit and observability exclude values and redact errors.

When an authorized human needs a secret-path read, the normal tool returns an opaque
access request. Approval occurs only through a loopback local admin channel whose random
token is printed to the local operator. Grants are path-bound, short-lived, non-reusable,
and redacted by default. Raw output requires an explicit local `--raw --confirm-raw`
decision. No MCP tool can approve a grant.

Content scanning is heuristic. It reduces accidental leakage but cannot identify every
possible secret format. Do not use a real secret as a test fixture.

## Authentication and public exposure

Stdio has no network listener. HTTP refuses to start without either OAuth or a recovery
bearer.

OAuth is the preferred public connector path. The public URL and passphrase must be set
together; half-configuration fails startup. With a durable state root, dynamic client
registrations and rotating refresh grants persist under `/state`. Authorization codes
and raw access tokens remain memory-only; only bounded SHA-256 access-grant digests may persist.

The static bearer is a **header-only recovery** mechanism. It is accepted through
`Authorization: Bearer`; query-string credentials are rejected even when the value is
correct. Keep the internal listener behind TLS reverse proxying and do not expose port
`8765` directly.

The console uses server-side OAuth completion, PKCE/state/single-use codes, and opaque
`Secure; HttpOnly; SameSite=Strict` sessions. Public landing content grants no MCP,
console, repository, deployment, or Edge authority.

The stable Front Door keeps OAuth discovery, authorization, token and RFC 7591
registration routing independent from MCP catalog admission. Catalog mismatch still
blocks `/mcp`, including SSE, while a healthy fixed backend can continue serving OAuth.
Rollouts may admit only the exact primary hash plus one authenticated transition hash;
wildcards, duplicates, malformed hashes and a third catalog fail closed.

## Edge trust and signed releases

The public control plane and a paired Edge authenticate their outbound protocol with a
per-device Ed25519 identity. The VPS sends only closed operations and opaque IDs. It has
no schema field for an arbitrary local path, command, script, URL, credential, target,
or caller-provided hash.

Official Edge releases are Ed25519-signed, component-hashed bundles. The manifest binds
release, exact source commit, protocol, catalog, architecture, and every authority-bearing
component. The signing private key is not installed on the Edge.

The package/updater stages and verifies the complete release, activates it atomically,
checks the fixed service, and restores the previous signed release on failure. Public
update tools select only the official stable channel or previous known signed release.
They cannot supply a URL, archive, executable, service, hash, or script. Repair restores
only official packaged components and fixed links/units.

A release in source control does not prove that production deployed it or a real Edge
installed it. Verify those states independently.

## Isolation profiles

### Public Layer-1 execution

Repository operations and allowlisted commands use application-level policy and the
daemon account. This is useful confinement but not OS isolation.

### L3 sandbox

`sandbox_exec` is unavailable unless a configured backend provides the reviewed
container boundary. The intended Docker profile has no network, read-only root,
workspace-only write access, dropped capabilities, no new privileges, and resource
limits. The public MCP container itself does not receive the Docker socket.

### Edge sandbox

The ordinary Edge `sandbox` profile runs OpenCode inside mandatory networkless
Bubblewrap. Only the selected workspace and a private runtime area are writable. Edge
identity, home, unrelated repositories, and private control sockets are excluded. There
is no direct-execution fallback.

### Managed browser harness

The browser capability is a general programming harness for every authorized development
workcell. It is not a remote-debugging endpoint and it is not limited to a fixed set of
click/type/select operations. `project_browser_harness_start` accepts an arbitrary argv
array in the persistent rootless toolbox, so a project may run Playwright, Puppeteer,
Selenium, WebDriver, browser CLIs, test runners, or custom automation in any installed
language. JavaScript and normal browser APIs are available through that project code.
The seven `project_browser_*` tools remain only a convenience wrapper for common Chromium
actions.

The harness reuses the existing toolbox boundary. The workspace is mounted at
`/workspace`; package managers may install browsers, drivers, libraries and utilities in
the persistent toolbox rootfs. The caller receives standard environment variables for a
run directory, arbitrary artifacts, downloads and a named persistent profile. Uploads
come from normal workspace files. Chromium, Firefox, WebKit or another engine may be used
when the installed framework supports it. MCP Devbox adds no domain allowlist, browser
action allowlist, JavaScript ban, download ban, upload ban or fixed-browser requirement.

The workcell remains the security boundary:

- Windows mounts and the general host home are not added;
- rootful Docker sockets and unrelated Edge state remain excluded;
- the toolbox may retain its already validated user-owned rootless engine socket, whose
  authority and limitations are the same as for all other toolbox workloads;
- caller argv is passed positionally to a fixed internal supervisor rather than
  interpolated into generated shell text;
- each run is bound to one project, target, toolbox and opaque `bh_...` identity;
- process status and stop revalidate Linux PID/start ticks before signaling;
- managed directories reject traversal, symlinks, foreign ownership and root escape.

The toolbox controls CPU, memory and process limits. Each harness run additionally has a
caller-selected timeout and combined managed run/profile storage ceiling. The supervisor
records bounded state and private logs, terminates the owned process tree on timeout or
storage exhaustion, and never infers cleanup from a chat ending. Completed/terminal run
metadata survives Edge restart; reopening the manager revalidates the live process in the
unchanged toolbox. An unexpected loss of process identity becomes `indeterminate` rather
than replaying uncertain browser effects.

Profiles and browser authentication data live under the ignored `.mcp-devbox/` workspace
state with owner-only parent directories. Public MCP output never returns cookie stores,
local/session storage, full profile trees, argv, environment, browser debugging addresses,
container names or host paths. Stdout/stderr are bounded and redacted. Screenshots, PDFs,
traces, videos, HARs, downloads and other files are enumerated only from relative
`artifacts/` and `downloads/` paths and read in exact bounded base64 chunks.

Full harness acceptance is host-specific because rootless container creation depends on
the target user's delegated systemd/cgroup-v2 subtree. GitHub Actions must not create a
root-owned transient unit, chown cgroup control files or pin a replacement engine merely
to imitate that authority. It records the exact hosted-runner limitation and fails on any
unexpected posture instead. Runtime acceptance is anchored to the owner-approved Edge
execution at `c27053c56b6214e52862ead675b874670f322295` and may be carried forward only
when the candidate changes no Edge, server, toolbox or browser-harness runtime code and a
fresh real-browser smoke still passes on `parrot-trusted-linux`. Any runtime change
requires a new exact E2E. The tested code receives no Edge identity or control-plane
credential.

The convenience Chromium session uses the authorized workcell's general HTTP/HTTPS
network and permits downloads to its managed profile. It retains a narrower filesystem
namespace for that convenience process, but it is not the programming boundary and does
not replace the arbitrary toolbox harness.

### Trusted Linux workcell

The trusted workcell shares the host network by design and may receive one verified
user-owned rootless Docker/Podman socket. It rejects rootful sockets and Windows mounts.
Filesystem/process containment, runtime labels, cancellation, and cleanup still apply,
but this profile is owner-trusted and is **not** universal target or egress isolation.

### Authorized target-locked workspace

The local contract adds one private target, VPN interface, machine metadata, and
authorization revision. Closed actions revalidate the exact route before use. Session
handles are bound to runtime/workspace/target/revision. Retargeting invalidates earlier
authority. Shared host networking still permits traffic outside the target; the target
contract is not nftables or overlay enforcement.

### Private validation runner

JavaScript validation runs in a separate private service, not in the public MCP
container. It accepts only fixed `pnpm-lockfile` and `pnpm-validate` profiles, repository
identifiers from a discovered allowlist, and a shared bearer. Child containers are
non-root, bounded, read-only, capability-dropped, and offline for validation. The
runner—not the public MCP—owns the narrowly reviewed Docker socket.

## Persistence and storage

Production state must be outside repository roots:

- `/repos`: agent-visible repository jail;
- `/state`: OAuth stores, task journal, audit, observability, telemetry, result store,
  model turns, Edge coordination, console sessions, and Brain console identity;
- `/brain`: Brain Markdown truth and local Git, with disposable search cache;
- `~/.local/state/mcp-edge`: private installed Edge identity, workspace registry, job
  journal, results, and optional local Git authority.

The image runs as UID/GID `10001:10001`; private state directories are `0700` and files
are `0600`. `/state` and `/brain` must never become repository aliases. OAuth files,
Edge identity, signing material, local Git credentials, private state, and Docker
sockets must never be exposed to the agent.

Back up authoritative state with ownership and modes preserved. Brain truth is
`.git`, `.gitignore`, `curated`, and `working`; `.cache` is disposable. Treat audit,
OAuth, Edge, result, and coordination state according to their retention and recovery
requirements rather than copying the entire tree without review.

## Redaction, audit, and observability

The append-only audit records tool, decision, bounded arguments, timing, and error class
after redaction. Structured observability has a closed schema for lifecycle, HTTP,
JSON-RPC, known tools, status, duration, byte counts, outcomes, and content-free
aggregates.

There is no free-form attribute map. Prompts, params, results, source, file content,
paths, repository names, commands, targets, URLs, headers, credentials, cookies,
identities, IPs, email addresses, raw errors, and model reasoning are excluded.

Large results are redacted before persistence and represented at the MCP boundary by an
opaque `result_ref` with bounded exact reads. The reference cannot address arbitrary
filesystem paths.

Trusted-workcell background processes reuse the foreground Bubblewrap launcher and its
workspace/cwd/environment checks. Their private Edge journal binds an opaque public id
to a local PID, process group and Linux start-time identity; only the private executor
uses those OS identities. Output is split by stream, redacted before private persistence,
bounded by administrator emergency limits and read incrementally. Stop signals only the
revalidated owned process group. Public tools never return PID, argv, environment,
workspace or log paths. Process state has no product TTL and cleanup is never inferred
from the end of a chat turn.

Durable process survival is explicit rather than accidental. Foreground Bubblewrap
keeps parent-death termination; background Bubblewrap omits it, and the Edge unit uses
`KillMode=process` so a service restart leaves reviewed per-process workers and their
groups running. The worker, not the restarting control loop, owns the stdout/stderr
pipes, redacts before writing and creates a private terminal receipt. On
reopen, the private manager revalidates PID, start ticks, process group, current Unix
owner and both no-follow private logs before treating a row as live. Reused or foreign
identities are never signalled. Missing/unsafe logs are demonstrated state corruption;
the still-owned group is killed fail-closed and the row becomes terminal. Public list
and cleanup responses remain metadata-only and never disclose the private journal.

Redaction is not a substitute for keeping secrets out of inputs and storage.

## Secure deployment checklist

- Start in `read-only`; use `ask` for reviewed changes.
- Configure only absolute repository roots and keep `/state` and `/brain` outside them.
- Run non-root; preserve private ownership and `0700`/`0600` modes.
- Use OAuth for public access, TLS termination, and header-only bearer recovery.
- Persist and test OAuth state across a rolling replacement.
- Keep credentials in the platform secret manager, never Git, prompts, logs, URLs, or
  command arguments.
- Keep the public MCP container free of Docker/rootful container sockets.
- Scope GitHub owner/repositories and Coolify apps/domains/mounts narrowly.
- Leave privileged profiles disabled unless a fixed reviewed profile is required.
- Verify `/healthz`, `/version`, `system_runtime_info`, exact commit, and profile-specific
  smokes.
- Verify signed source release, deployed control plane, and installed Edge separately.
- Retain rollback, backup, revocation, and incident-response procedures.

## Known limitations

- Layer-1 allowed commands inherit the daemon user's ambient OS access.
- Secret content detection is heuristic.
- The trusted Linux workcell shares host networking and can consume owner resources.
- A rootless engine still grants broad authority within that user's namespace.
- Persistent toolbox services expose only server-generated identities and lifecycle
  state. Their private PID is always paired with Linux process start ticks before
  status, TERM or KILL, and caller argv is passed positionally to a fixed internal
  supervisor script rather than interpolated into shell text. Service commands are not
  persisted for unattended replay after a container restart.
- Persistent toolbox CPU, memory and process limits are closed integer inputs with
  server-owned defaults and maxima. The applied values are stored in owner-only state
  and revalidated against the live rootless container before use; limit drift is an
  ownership failure, not an invitation to update the container implicitly.
- A toolbox may receive only the user-owned rootless engine endpoint already validated
  by the Edge. The internal socket path and endpoint variables are fixed; ownership
  revalidation requires exactly the workspace and socket binds and rejects extra
  mounts or environment drift. This authority can consume the user's rootless CPU,
  memory, disk and network, so it remains confined to explicitly registered `dev`
  workspaces and is never exposed by the public result.
- Target-locking is a closed operational contract, not universal egress filtering.
- Signed bundles prove artifact identity, not correctness of every dependency or host.
- A compromised administrator, host kernel, reverse proxy, signing key, or external
  integration can defeat the corresponding boundary.
- Resource limits reduce denial-of-service risk but cannot guarantee availability.
- The model can damage data inside its selected writable workspace.
- No formal verification or universal cross-platform OS sandbox is claimed.

## Security tests and evidence

Security behavior is protected by unit, adversarial, race, fuzz, integration, package,
container, OAuth, documentation, and exact-head CI contracts. Important coverage
includes traversal/symlink handling, secret denial/redaction, command parsing, plan
expiry/replay/revalidation, OAuth state, result bounds, signed manifests, Edge protocol,
Bubblewrap profiles, target/session binding, rootless socket validation, cancellation,
and rollback.

Run the repository gates from the exact commit:

```bash
go test ./... -count=1
go vet ./...
go build ./...
git diff --check
```

Dated closure evidence belongs under [`baselines/`](baselines/). Live deployment identity
comes from `/version` or `system_runtime_info`; the current public tool contract comes
from [`tools.md`](tools.md). Historical evidence must not be rewritten to match current
state.
