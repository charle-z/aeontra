# L3 Sandbox Plan

Status: accepted direction, implementation pending.

## Goal

Let ChatGPT run broader build commands without turning the public MCP endpoint into
a free terminal. L3 exists so commands can be more capable while still being
confined by OS isolation, default-deny egress, and audit.

## Non-Negotiable Requirements

- no free terminal before L3
- human approval remains required for risky command execution in `ask` mode
- default-deny egress for sandboxed commands
- block metadata and private networks by default, including `169.254.169.254` and
  RFC1918 ranges
- preserve the app-level policy gate: jail, secret deny, command allowlist, redaction,
  and audit still run before a command reaches the sandbox
- no Docker socket in the public MCP container
- grants remain local-human-only and are not part of the sandbox API

## Decision

The public `mcp-devbox` HTTP container must not own host-level sandbox power. In
particular, do not mount `/var/run/docker.sock` into the internet-facing MCP
container just to let it run `docker run`; Docker socket access is effectively
host-root and would make the MCP daemon a high-value escape target.

Instead, L3 is a separate runner boundary:

1. `mcp-devbox` keeps exposing MCP and policy.
2. A local, host-controlled sandbox runner owns the privileged isolation mechanism
   (Docker/gVisor/nsjail/bubblewrap depending on the host).
3. The daemon talks to the runner through an explicit runner contract, not a shell.
4. The runner receives only: workspace path, argv, environment allowlist, network
   profile, timeout, and resource limits.
5. The runner returns only: exit code, stdout/stderr, duration, and sandbox metadata.

That contract lets us add broad build commands later without giving the model a
general-purpose host control plane.

## Runner Contract

The explicit runner contract is:

```text
Run(ctx, request) -> result

request:
  cwd: exact workspace path already resolved by Policy
  argv: explicit program + args, never a shell string
  env: allowlisted environment variables only
  network: none | allowlist
  allow_egress: explicit hostnames/CIDRs when network=allowlist
  timeout: bounded duration
  resources: cpu/memory/pids/filesize limits

result:
  exit_code
  stdout
  stderr
  duration
  sandbox_backend
  egress_profile
```

The daemon must still redact stdout/stderr before returning output to the agent.

## Backend Order

Preferred backend order:

1. **Host-managed gVisor/Docker runner** on VPS/Linux. Good isolation if configured
   on the host, not by mounting Docker socket into the public MCP container.
2. **nsjail/bubblewrap runner** when the host allows required namespaces/capabilities.
3. **Plain exec** remains L1 only and must not be described as L3.

Coolify note: the current app container is non-root and capable enough for Go/git,
but it is not an OS sandbox for child processes. Treat it as L1/L2 app policy, not
as L3.

## Egress Policy

Default network profile is `none`. Build tools that need network must declare an
egress allowlist such as:

- `github.com`
- `proxy.golang.org`
- `sum.golang.org`
- selected package registries for the repo

Always block:

- `169.254.169.254`
- RFC1918 ranges
- loopback addresses other than the runner's own control channel
- link-local and multicast ranges

## Implementation Steps

1. Add a `SandboxRunner` interface beside the current `Runner`, plus tests that
   prove command tools cannot opt into broad execution unless a sandbox backend is
   configured.
2. Add a `sandbox_status` diagnostic or startup log that clearly says whether L3 is
   active. If inactive, broad/free commands must remain unavailable.
3. Implement the first Linux runner backend behind an explicit config flag/env var.
4. Add adversarial tests for workspace escape, secret exfil through permitted
   commands, network egress to metadata/RFC1918, timeout, and resource limits.
5. Only after those tests pass, consider a broader command tool. Until then,
   `run_command` remains allowlist-only and audited.
