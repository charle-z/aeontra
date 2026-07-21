# Security Policy

## Honest scope (under-promise)

mcp-devbox is **secure-by-default**, not "secure". It raises the bar above a
permissive local-development MCP through read-only defaults, secret denial by path
and content, a closed command allowlist, a filesystem jail that also covers command
execution, patch-first writes, planned consequential actions and audit logging.

It does not provide one universal isolation or egress guarantee. Security posture is
profile-specific:

- the public control-plane command path is Layer-1 application policy, not an OS
  sandbox;
- the ordinary Edge `sandbox` profile runs OpenCode inside mandatory networkless
  Bubblewrap with no direct-execution fallback;
- the trusted `linux-workcell` profile deliberately declares
  `trusted_host_shared_network` and is suitable only for owner-controlled development
  and explicitly authorized laboratories;
- target-locked HTB sessions restrict structured actions to one locally registered
  private target and revalidate its VPN route, but this is not general host egress
  filtering.

### What Layer 1 enforces

- Read-only by default; writes and commands require `--mode ask|allow`, and risky
  actions require approval in `ask` mode.
- Secrets are denied by path (`.env`, `.ssh`, keys, credentials and similar paths)
  and returned payloads pass through content redaction.
- Commands are allowlist-only, use no free shell, and reject destructive or injection
  forms.
- Filesystem and command operations remain inside configured roots with traversal and
  symlink defenses.
- Repository contents are treated as **data, never instructions**.
- Runtime policy is loaded by the administrator and cannot be weakened by an agent.
- GitHub, Git publication, Coolify, notes, privileged profiles and other
  consequential actions use exact TTL-bound single-use plans with state revalidation
  and audit.
- Publication has no force, mirror, tag, caller URL, arbitrary refspec or embedded
  credential surface.
- Privileged profiles are server-defined and disabled by default. The public MCP is
  never given a free host terminal or Docker socket.

### Known limitations

- A permitted Layer-1 command runs as the daemon user. Application policy cannot stop
  that child from reading everything available to the same account.
- The trusted Linux workcell shares the host network. It is not universal target or
  egress isolation.
- Content secret scanning is heuristic; it reduces leakage risk but cannot guarantee
  that every possible secret format is detected.
- Bubblewrap, signed bundles and target-locked actions reduce specific risks; they do
  not turn the whole platform into a formally verified sandbox.

Use `--mode read-only` unless writes or commands are needed, and prefer `ask` over
`allow`.

Compatibility aliases do not relax policy: old and recommended names share the same
schema, handler, checks, planning, approval posture and audit path. Tokens come only
from administrator or private Edge configuration and are never returned.

## Remote control-plane transport

- The production HTTP endpoint supports OAuth as the preferred ChatGPT path.
  `Authorization: Bearer` is a header-only recovery path; query-string credentials
  return 401.
- `/healthz` and safe build/catalog identity are intentionally bounded. Other MCP and
  console routes require authentication according to their documented contract.
- Production is exposed through the existing TLS reverse-proxy path. Exposing any
  control plane to a network widens the attack surface; rotate credentials after a
  suspected leak and review content-free audit/observability events.
- HTTP, stdio and console-facing operations reuse the same policy, redaction and
  authority boundaries rather than duplicating security checks.

## Edge and authorized-lab boundary

P15 Edge releases are Ed25519-signed, component-hashed bundles. The restricted updater
accepts only the official stable channel, stages and verifies the complete release,
activates it atomically, health-checks it, and supports bounded rollback or repair.
The installed Edge contains no signing private key, and callers cannot provide an
update URL, script, command, path or hash.

P14/P15 authorized HTB actions are available only inside a registered `htb-linux`
runtime. The workspace is bound locally to one Edge identity, private IPv4 target,
VPN interface and authorization revision. Credential values and saved sensitive
output remain local; the control plane receives opaque handles or metadata only.
Retargeting invalidates previous session authority. These controls must never be used
against systems without explicit authorization.

The development Edge Git broker uses a separate local credential stored in private
0600 Edge state. It constructs only owner-bound repository URLs, disables credential
helpers, hooks, fsmonitor commands and the file protocol, and requires a short-lived
single-use publication plan. The credential is not mounted into the workcell or sent
through model schemas, argv, responses or logs.

## Reporting a vulnerability

Report suspected vulnerabilities privately to the maintainer rather than opening a
public issue. Include reproduction steps and the affected release or commit. Reports
will be acknowledged as promptly as practical, and reporters may be credited when
requested.

This repository has no open-source `LICENSE`. Public visibility does not grant rights
to use, copy, modify or distribute it. No security guarantees are made.
