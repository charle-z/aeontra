# Security Policy

## Honest scope (under-promise)

mcp-devbox is **secure-by-default**, not "secure". It raises the bar well above a
permissive local dev MCP (read-only default, secret deny by path **and** content,
command allowlist with destructive/injection blocking, a path jail that also covers
command execution, patch-first writes, and an audit log). It does **not** yet provide
OS-level isolation or egress control — those are Layer 3.

### What Layer 1 enforces
- Read-only by default; writes/commands require `--mode ask|allow`; risky actions ask.
- Secrets denied by path (`.env`, `.ssh`, keys, creds…) and redacted by content scan
  on every returned payload (file reads, search, command output, diffs, memory, audit).
- Commands: allowlist-only, no shell, destructive/injection forms blocked.
- Filesystem + command jail confined to configured roots (symlink/traversal safe).
- Repo file contents are treated as **data, never instructions**.
- Policy is loaded once and is **not** modifiable by the agent at runtime.

### What Layer 1 does NOT yet do (known limitations)
- **No OS sandbox (Layer 2).** A permitted command runs as your user; app-level policy
  cannot stop a permitted process from reading files itself. Run untrusted workloads
  under OS isolation (planned: gVisor/nsjail/Docker on Linux/WSL2).
- **No egress control (Layer 3).** A permitted command could reach the network.
- Content secret-scanning is heuristic; it reduces but cannot guarantee zero leakage.

Use `--mode read-only` (the default) unless you need writes/commands, and prefer
`ask` over `allow`.

### Remote HTTP transport (v0.2)
- The HTTP endpoint (`serve --http`) requires a bearer token on every `/mcp` request
  (constant-time check, fail-closed if no token is configured); `/healthz` is the only
  unauthenticated route and exposes no information.
- It binds to `127.0.0.1` by default; remote access is intended **only** via a
  self-hosted Cloudflare Tunnel (outbound, no inbound ports). Add Cloudflare Access
  for a second auth gate where possible.
- Exposing the daemon to a network widens the attack surface. Use a long random token,
  prefer `read-only`/`ask`, rotate the token if leaked, and watch the audit log. All
  L1 invariants (jail, secret deny + redaction, allowlist, audit) apply to HTTP requests
  exactly as to stdio — the transport reuses the same policy gate.

## Reporting a vulnerability

Please report suspected vulnerabilities privately to the maintainer rather than
opening a public issue. Include reproduction steps and the affected version/commit.
We aim to acknowledge reports promptly and will credit reporters who wish it.

This software is provided "as is" (see LICENSE); no security guarantees are made.
