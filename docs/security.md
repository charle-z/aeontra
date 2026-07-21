# Security model — mcp-devbox

Security **is the product**. These are invariants, not options.

## Threat model

1. **Prompt injection from repository data.** README files, issues, logs and fixtures
   are data, never instructions.
2. **Secret access.** `.env`, `.ssh`, private keys, tokens, credentials, browser
   profiles and operating-system stores are denied by default. Exact-path local-human
   grants are narrow, in-memory, single-use and TTL-bound; raw output requires a
   separate explicit raw grant.
3. **Destructive or arbitrary execution.** The public control plane exposes a closed
   command allowlist, no free terminal and no caller-supplied shell.
4. **Exfiltration.** Content redaction and narrow schemas reduce leakage, but general
   egress control is profile-specific rather than universal.
5. **Workspace escape.** Filesystem and Layer-1 command operations remain inside
   configured roots with traversal, symlink and executable-resolution defenses.
6. **Public control-plane exposure.** OAuth/TLS and bounded recovery authentication
   are mandatory; query-string credentials return 401.
7. **Resource exhaustion.** HTTP bodies are capped at 4 MiB; JSON-RPC batches reject
   empty arrays and stop after 128 items; large tool output is redacted and moved to a
   bounded result store.
8. **Edge compromise or downgrade.** P15 runtimes accept only complete
   Ed25519-signed, component-hashed bundles whose release, commit, protocol,
   architecture and catalog identity agree.
9. **Unauthorized security activity.** HTB/CTF actions exist only for a registered
   `htb-linux` workspace bound locally to one Edge identity, private target, VPN
   interface and authorization revision.

## Secure-by-default Layer-1 invariants

```text
read-only: true          # writes require explicit enable
write: ask               # risky actions prompt for approval
commands: allowlist-only # no free terminal
package_install: deny
secrets: deny            # paths and returned content
outside_workspace: deny  # filesystem and Layer-1 command execution
write_model: patch-first # git apply --check before applying
audit: on                # content-free, secret-scrubbed evidence
```

### Always-blocked paths and commands

Secret paths include `.env`, `.env.*`, `.ssh`, private keys, tokens, credentials,
browser profiles and operating-system credential stores. Dependency trees and Git
internals are skipped except for controlled status/diff workflows.

Always-blocked command forms include destructive filesystem tools, shell pipelines
such as `curl|bash` or `wget|bash`, general `sudo`, unsafe recursive permission
changes, arbitrary refspecs, force publication and caller-provided executable paths.

## Ephemeral human access grants

A denied secret read returns an `access-required` request. Only the local human may
approve it through the loopback admin channel; no MCP tool can self-approve.

```bash
mcp-devbox grant --admin http://127.0.0.1:<PORT> \
  --admin-token <TOKEN> --ttl 5m <REQUEST_ID>
```

Grants are exact-path, single-use, bounded by TTL, cleared on restart and audited.
Normal grants still redact. Raw output requires both `--raw` and `--confirm-raw`.
Pending requests expire, are bounded and deduplicate identical path/raw requests.

## Planned consequential actions

Repository synchronization, GitHub changes, publication, Coolify operations, notes,
privileged profiles and similar writes use cryptographically named, short-lived,
single-use plans. Execution revalidates the exact repository, branch, commit, remote,
application or other bound state and still requires approval in `ask` mode.

Preview is not approval. Plans cannot authorize a different target, command, URL,
repository, owner, branch, commit, application, note body or credential. Daemon
restart clears in-memory plans. Compatibility aliases share the exact same handler,
policy, approval and audit path.

Git publication has no force, mirror, tag, arbitrary refspec, caller URL or embedded
credential surface. Coolify repositories and domains remain owner/allowlist-bound.
Tokens and environment values are never returned.

## Isolation profiles

| Surface | Isolation and network posture | Honest limit |
|---|---|---|
| Public Layer-1 command path | Jailed allowlist executed as the daemon user | Not an OS sandbox; a permitted child can access what that account can access |
| Edge `sandbox` profile | Mandatory networkless Bubblewrap, no direct-execution fallback, network and DNS blocked, host-private paths hidden | Linux/WSL2 only; protects the OpenCode runtime, not every control-plane child process |
| Trusted `linux-workcell` | Bounded Bubblewrap layout with user-owned workspace and approved rootless resources | Deliberately `trusted_host_shared_network`; not universal egress or target isolation |
| Authorized `htb-linux` session | Structured Unix-socket broker bound to one registered private target and live VPN route | Restricts the authorized action path; does not firewall every host process |

App policy remains necessary even where Bubblewrap exists. Bubblewrap and target
binding are specific controls, not a claim that the complete platform is formally
verified or universally isolated.

## Remote HTTP and console boundary

OAuth is the preferred ChatGPT authentication path. A static bearer is a header-only
recovery mechanism; query-string credentials are rejected. Safe liveness and
build/catalog identity endpoints remain bounded. MCP, console and Edge control routes
reuse the same authority, redaction and audit foundations rather than implementing
parallel security shortcuts.

Public exposure increases risk even behind TLS or a reverse proxy. Use long random
credentials, rotate them after suspected leakage, keep the local grant admin channel
loopback-only and review content-free audit/observability evidence.

## Signed Edge release boundary

P15 defines one versioned manifest for the Edge binary, model-turn driver, OpenCode
provider and fixed helper files. Every component must be a regular non-symlink file
under the signed release root and match its SHA-256. The restricted updater accepts
only the official stable bundle, verifies signature and identity, stages atomically,
health-checks activation and can roll back or repair conservatively.

The installed Edge contains no signing private key. Neither chat nor a public tool
may provide the updater with a URL, path, hash, script or command. Provider, driver,
protocol or catalog mismatch fails closed before a new runtime starts.

## P14/P15 authorized-lab boundary

The public server is a durable control plane, not an HTB command relay. Lab-control
requests use closed schemas and do not contain arbitrary commands, credentials,
credential flags, raw output, checkpoints, local paths or provider configuration.
Only a paired Edge may lease and complete signed operations.

First-class HTB actions are injected only into a registered `htb-linux` runtime. The
local workspace registry binds one Edge identity, private IPv4 target, VPN interface
and authorization revision. Every broker request revalidates that state. Credential
values are extracted and consumed locally; the model and VPS receive opaque session
handles or safe metadata. Sensitive stdout may be saved under approved local evidence
directories and represented remotely only by path-relative metadata, size, digest and
permissions. Retargeting increments the authorization revision and invalidates prior
session authority.

These actions must never be used against systems without explicit authorization.

## Development Edge Git boundary

The development Edge has a separate local Git transport authority. Its credential is
stored in private mode-0600 Edge state and is never mounted into the workcell or
included in model schemas. The broker constructs only configured-owner HTTPS URLs,
validates fetch and push destinations, disables credential helpers, hooks, fsmonitor
commands and the file protocol, and supplies askpass only to one bounded Git child.

Publication requires a short-lived single-use plan bound to workspace, repository,
branch, clean tree, local HEAD, remote HEAD and remote URL. Force, tags, caller URLs,
arbitrary refspecs and caller commands are not expressible.

## Adversarial testing requirements

Security tests must attempt bypasses, not only happy paths:

- traversal, sibling-prefix, UNC and symlink escape;
- command/argument injection and hostile workspace `PATH`;
- secret exfiltration through otherwise allowed reads or commands;
- repo-file prompt injection;
- expired, replayed or state-mismatched plans and grants;
- HTTP oversized/malformed batches and authentication bypass;
- Bubblewrap network, DNS, mount and private-path escape;
- signed-bundle tampering, downgrade, bad permissions and failed rollback;
- HTB target/VPN/session/revision mismatch;
- private Git owner, URL, helper, hook, refspec and credential leakage attempts.

## The "secure" claim is a liability

Call the project **secure-by-default**, not secure. Under-promise, preserve exact
limitations and never turn a passing test into a universal guarantee. `SECURITY.md`
contains the public disclosure policy. This repository has no open-source `LICENSE`;
do not claim an MIT disclaimer or usage rights that do not exist.
