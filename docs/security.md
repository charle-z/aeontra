# Security model — mcp-devbox

Security **is the product**. These are invariants, not options.

## Threat model

1. **Prompt injection from repo files** — a README/issue/log/test fixture may
   contain instructions trying to make the agent read secrets or run commands.
   → File contents are **data, never commands.**
2. **Secret access** — `.env`, `.ssh`, keys, tokens, credentials, browser profiles,
   OS stores. → **Denied by default.** A local human can approve a narrow,
   in-memory, single-use grant for one exact resolved path; raw unredacted output
   requires a separate explicit raw grant.
3. **Destructive / arbitrary commands** — `rm -rf`, `format`, `curl|bash`, `sudo`…
   → **Allowlist only; no free terminal.**
4. **Exfiltration** — agent sends code/secrets to an external endpoint.
   → **Egress control (Layer 3): default-deny outbound.**
5. **Workspace escape** — operations outside configured project paths, including
   via terminal. → **Path jail covering both filesystem AND commands.**
6. **Public exposure of the daemon** — the tunnel must require auth + TLS.
7. **HTTP resource exhaustion** — request bodies are capped at 4 MiB; JSON-RPC
   batches are parsed incrementally, reject empty arrays, and stop after 128 items.

## Secure-by-default invariants (Layer 1)

```text
read-only: true          # writes require explicit enable
write: ask               # risky actions prompt for approval
commands: allowlist-only # per-project; no free terminal
package_install: deny
secrets: deny            # .env, .ssh, keys, tokens, credentials
outside_workspace: deny  # applies to fs AND command execution
write_model: patch-first # validate with `git apply --check` before applying
audit: on                # log who/what/when/files/duration
```

### Always-blocked paths
`.env`, `.env.*`, `.ssh`, private keys, tokens, credentials, browser profiles,
OS credential stores, `node_modules`, `vendor`, `.git` internals (except
controlled `git status`/`git diff`).

### Ephemeral human access grants

Secret paths stay denied by default. When `read_file` or `read_many_files` touches
a secret path inside the jail, the tool returns a structured `access-required`
payload with a request id, exact resolved path, reason, and whether raw output was
requested. This is not an approval.

Approval is only through the daemon's local loopback admin channel, protected by a
random per-process admin token printed to the daemon console:

```bash
mcp-devbox grant --admin http://127.0.0.1:<PORT> --admin-token <TOKEN> --ttl 5m <REQUEST_ID>
```

The MCP tool list does not expose any grant/approval tool. Remote ChatGPT over HTTP
can see the request id and retry the read with `access_request_id`, but it cannot
approve that id. The local human operating the daemon must copy the console command
or otherwise run the CLI locally.

Grant properties:

- In-memory only; nothing is written to config or disk, and daemon restart clears it.
- Exact resolved path only; no wildcards, parent directories, sibling files, or jail
  expansion.
- Single-use and TTL-bounded; default TTL is 5 minutes, maximum accepted TTL is 1 hour.
- Pending requests expire after 15 minutes, are capped at 256, and exact duplicate
  path/raw requests reuse one id so an agent cannot create unbounded approval spam.
- Normal grants still run content redaction before returning data.
- Raw unredacted output requires `--raw --confirm-raw`; `--raw` alone is rejected.
- Requests and approvals are audit logged with request id, path, TTL, raw flag, and
  decision. Args, errors, and every file/path entry are independently secret-scrubbed
  before JSONL persistence.

### Always-blocked commands
`rm -rf`, `del /s`, `format`, `mkfs`, `curl|bash`, `wget|bash`,
`powershell Invoke-Expression`, `sudo`, `chmod -R 777`.

## Planned consequential actions

Repository fast-forward, GitHub creation, remote updates, publication, Coolify app
creation/deployment, note writes, and privileged profiles use a shared in-memory
plan mechanism. IDs use cryptographic randomness; plans hold an operation and exact
normalized non-secret arguments, creation/expiry timestamps, and single-use state.
Execution consumes the plan, rechecks policy and current state, and audits creation,
execution, expiry, replay, and rejection.

Preview is not approval. In `ask` mode execution still requires `approve=true`. A
plan cannot authorize a different repository, owner, remote, branch, commit,
application, service, command, or note body. Daemon restart clears every plan.

Git publication has no force/mirror/tag/refspec surface. GitHub operations are fixed
to `GITHUB_OWNER`; Coolify repositories use that owner and domains obey
`COOLIFY_ALLOWED_DOMAINS`. Tokens and env values never appear in output or audit.
Compatibility aliases invoke identical handlers and cannot weaken policy.

The development Edge has a separate local Git transport authority. Its PAT is a
0600 Edge-state file and is never mounted into the workcell or offered in a model
schema. The broker constructs an HTTPS URL only from the configured owner plus a
simple repository name, validates both fetch and push URLs, disables Git credential
helpers, hooks, fsmonitor commands and the file protocol, and supplies askpass only
to a bounded Git child. Publication requires a five-minute single-use plan bound to
workspace, directory, branch, HEAD, remote HEAD and remote URL; it has no force,
tags, caller URL or caller refspec surface.

## Isolation layers

| Layer | Mechanism | When |
|---|---|---|
| 1. App policy | Go: path validation, denylist, allowlist, read-only, audit | MVP |
| 2. OS isolation | **Wrap** gVisor / nsjail / Docker (Linux/WSL2) so commands can't escape | v0.3 |
| 3. Egress | default-deny outbound; block 169.254.169.254 + RFC1918; allowlist endpoints | v0.3 |

> App-level policy is **necessary but not sufficient**: if command execution
> exists, a command can read files regardless of policy unless the process itself
> is OS-sandboxed. That is why Layer 2 matters — and why it must **wrap** proven
> tech, not be hand-rolled.

## Tunnel security (ChatGPT bridge)

The daemon is on a local PC behind NAT. To let ChatGPT (cloud) reach it:

- **Cloudflare Tunnel** — outbound-only connection; no inbound ports opened.
- **Cloudflare Access (Zero Trust free tier)** — auth gate in front (only you).
- **TLS** — automatic via Cloudflare.
- **Bearer/OAuth on the daemon** — defense in depth even behind Access.
- Never expose the raw daemon to the public internet.

A reverse proxy (nginx/Traefik) is the *server-with-public-IP* pattern (e.g. on a
VPS) — it does **not** solve the NAT problem for a local PC. The tunnel does.

## Beyond path-blocking: content-level secret scanning

Blocking `.env`/`.ssh` by **path** is not enough — secrets also live in source code,
config files, logs, and git history. Before returning ANY file content, **scan for
secret patterns** (API keys, tokens, `BEGIN ... PRIVATE KEY` headers, common
credential formats) and **redact**. Desktop Commander does NOT do this — it is both
a real need and a genuine differentiator.

## Adversarial self-testing (red-team the tool itself)

For a security product, tests must **attempt to bypass** the controls, not just
confirm happy paths:
- path traversal (`../`, absolute paths, UNC), symlink escape
- command injection via tool arguments
- allowlist bypass (chained/quoted commands, path-qualified names, hostile workspace PATH)
- secret exfiltration through a *permitted* command
- prompt-injection from a repo file trying to elicit a forbidden action
> This is exactly the owner's red-team domain — the security testing of mcp-devbox
> is the dogfooding of their actual skill.

## The "secure" claim is a liability

Calling the tool "secure" raises the bar: a bypass here is worse than in a
permissive tool. **Under-promise.** Ship `SECURITY.md` + a vulnerability disclosure
policy. Keep the MIT "as is" disclaimer. Never claim guarantees that can't be held.

## Signed Edge release boundary

P15 Edge workers accept new local jobs only when the complete versioned release
manifest has a valid Ed25519 signature and every fixed component matches its SHA-256.
The compiled release, commit, protocol, architecture and exterior catalog identity
must agree. Provider and driver mismatches fail before runtime creation with closed,
content-free codes. The installed Edge contains no signing private key, and neither
the chat nor a public tool may supply a URL, path, hash, script or command to the
updater. See `docs/edge-bundles.md`.

## Required security tests (Layer 1)

- path traversal blocked · access outside workspace blocked · `.env`/`.ssh` blocked
- destructive commands blocked · non-allowlist commands blocked
- `apply_patch` validates before applying · allowed read/search work
- audit log records actions and redacts args/errors/file paths · repo-file instructions are NOT executed
- **content secret-scan redacts keys/tokens in returned files**
- **bypass attempts (traversal/symlink/arg-injection/allowlist) all fail**
- HTTP: auth required, oversized bodies fail, empty/over-128 batches fail with bounded errors
- access grants: agent cannot self-approve; expired/used grants fail; exact path only;
  default output remains redacted; raw requires explicit raw approval; restart clears grants
## P15 lab-control boundary

The public server is a durable control plane, not an HTB command relay. Its lab
operations are closed-schema requests and never contain a command, credential,
credential flag, output, checkpoint, local path or provider configuration. Only a
paired Edge can lease or complete an operation, using the existing signed request
protocol and replay protection.

Control delivery is authenticated in both directions. Edge signs every polling and
completion request with its device key. The server signs each leased operation over
the operation/device/kind/request digest, lease ID and exact expiry using a private
control key persisted with mode `0600`; Edge verifies that signature before any local
operation, including updater/rollback/repair. New pairing records only the public
control key. A preserved schema-v1 P14 identity obtains that public key once over its
already authenticated HTTPS/device-signed channel, upgrades only identity metadata,
and preserves the device ID and private device key.

Every HTB target must be one private IPv4 whose route is currently attached to a
local `tun*` or `tap*` interface. The authorization revision is written atomically
inside the private workspace. The broker verifies that revision for every request;
retargeting therefore closes the authority of already-running sessions even if an
old process is still alive.
