# Private edge workcells

MCP Devbox edge workcells are outbound-only execution environments for a personal PC, WSL, Parrot, or a dedicated lab host.

They exist to preserve operational flexibility without giving an AI provider unrestricted authority over the machine.

## Why workcells instead of one wrapper per tool

A fixed wrapper such as `nmap_service_scan` is easy to audit but becomes too narrow when a real task needs combinations of discovery, packet capture, scripting, parsing, browser interaction, Kerberos tooling, Burp extensions, or custom code.

A workcell therefore exposes a broader installed toolchain to a local agent process, while authority remains outside that process.

```text
ChatGPT or private orchestrator
            |
       MCP Devbox core
            |
  exact task + engagement claims
            |
      outbound edge channel
            |
   WSL / Parrot workcell
      |             |
 local agent     toolchain
 OpenCode/etc.   nmap, Python,
                 Kerberos, Burp bridge,
                 source tools, browsers
```

The local agent may choose commands and combine tools inside the workcell. It cannot enlarge filesystem roots, network scope, duration, concurrency, credentials, or privilege level.

## Authority boundary

Every workcell task is bound to:

- one device identity;
- one workspace or engagement;
- allowed filesystem roots;
- allowed destination scope and protocols;
- network, CPU, memory, process, and time budgets;
- permitted privilege class;
- evidence retention and redaction rules;
- an expiry and cancellation token;
- a human approval when the action is consequential.

The edge revalidates these claims locally before execution. Repository files, prompts, and model output cannot grant authority.

## Agent adapters

The workcell can host an adapter for a local agent such as OpenCode, a future provider CLI, or a provider-neutral task runner.

The adapter receives a structured goal and bounded environment, not infrastructure secrets. It returns:

- a concise result summary;
- structured observations;
- file or evidence references;
- commands executed and exit status, subject to redaction;
- an explicit completed, failed, cancelled, or approval-required state.

Raw evidence stays in the private workcell or evidence store. GPT receives only the minimum necessary content.

## Privileged operations

Passwords never pass through ChatGPT, MCP results, prompts, or audit logs.

A privileged action follows this flow:

```text
agent proposes exact privileged effect
-> MCP Devbox creates a short-lived challenge
-> local user approves through Windows Hello, WebAuthn, or a local prompt
-> root helper validates the signed challenge
-> helper executes one allowlisted effect
-> capability is consumed
-> sanitized result returns to the task
```

The root helper listens only on a local Unix socket and accepts profile identifiers plus validated parameters. It never accepts a free shell string.

## Startup and hardening

On Windows, Task Scheduler starts WSL at boot or login. Inside WSL, a systemd service maintains the edge process.

Minimum posture:

- dedicated non-root user;
- outbound-only transport;
- per-device credentials and revocation;
- `NoNewPrivileges=true`;
- restricted writable paths;
- no Docker socket by default;
- local kill switch;
- bounded logs with secret redaction;
- automatic restart on failure, not endless restart on invalid configuration;
- explicit update and rollback procedure.

## Security and infrastructure use

Development, infrastructure, and authorized-security workcells share the same transport and authority model but use separate policies and data stores.

Examples:

- development: repositories, tests, compilers, package managers;
- infrastructure: Terraform/OpenTofu plan, Ansible check mode, diagnostics, observability;
- security: approved lab or engagement scope, Parrot toolchain, evidence handling.

Infrastructure `apply`, active security testing, package installation, service changes, and privileged operations remain separate approval classes.

## Initial implementation order

1. Device identity, outbound transport, heartbeat, revocation, and cancellation.
2. One development workcell with a jailed repository root.
3. Provider-neutral task/result protocol and an OpenCode-compatible adapter.
4. Local approval helper for exact privileged profiles.
5. Infrastructure workcell with Terraform/OpenTofu validation and plan only.
6. Authorized Parrot workcell with engagement-bound network enforcement.
7. Burp bridge and richer interactive evidence workflows.

This design keeps MCP Devbox useful for complex real work without turning it into an unaudited remote terminal.

## P12 Trusted Linux Workcell implementation

P12 implements one explicit `linux-workcell` profile with default `dev` behavior and optional local `htb-linux` metadata. It does not create separate development and HTB execution profiles.

The profile reuses the outbound P11.2 relay and opaque workspace IDs, but policy selection is local to Edge. The existing `sandbox` remains networkless and rejects shared networking. `linux-workcell` is displayed as `TRUSTED LINUX WORKCELL` and declares `trusted_host_shared_network` because this version intentionally shares the Parrot host network.

The implementation provides:

- strict Linux workspace roots and explicit registration opt-in;
- rendered local instructions, resumable current state, and sanitized tool inventory;
- local HTB interface, IPv4, route, and LHOST preflight;
- workspace-local package prefixes;
- optional user-owned rootless Docker or Podman sockets under `/run/user/<uid>`;
- exact runtime-labelled cleanup and terminal checkpoints.
