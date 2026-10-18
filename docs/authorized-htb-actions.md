# First-class authorized HTB and CTF actions

P14 exposes explicit structured actions for Hack The Box and controlled CTF Linux
laboratories. They are not disguised development commands. They are available to the
external model only when the selected workspace is a registered `linux-workcell` in
`htb-linux` mode and the Edge has started the private runtime broker.

The operator grants authorization once with `mcp-edge lab init`. That registration
binds one workspace to one Edge identity, one IPv4 target, one VPN interface and the
`htb-linux` mode. Every operation revalidates the VPN route and uses only the target
stored in the local signed workspace registry. No action accepts `target`, `host`,
`ip`, password, private key, forwarding, proxy or mount fields.

These tools must never be used against systems that are not explicitly authorized.
They are intended for Hack The Box, CTFs and controlled security laboratories only.

## Runtime-only execution boundary

The six names are present in the deterministic public MCP catalog so their purpose,
schemas and annotations are reviewable. Direct calls on the VPS fail closed after
workspace authorization validation because the VPS cannot see Parrot's route,
credential files or broker. During an `htb-linux` runtime, Edge injects the same six
definitions into the private OpenCode provider. The provider sends structured JSON
over the runtime-owned Unix socket and consumes the safe tool result internally.
It never builds a Bash command and never calls `mcp-edge lab ssh-exec` through the
model's shell tool.

The provider does not inject these tools in `dev` or sandbox workspaces. When an
external-model response mixes HTB and ordinary tool calls, the provider executes
only the structured HTB calls, returns their results to the model, and defers the
ordinary work to a later response. This preserves unambiguous execution and result
attribution without terminating the runtime for a recoverable model mistake.

## Tools

### `workspace_htb_status`

Input: `workspace_id` only. Returns safe authorization, VPN and broker state, the
current remote username when a live session exists, opaque session handles, metadata
for local `loot/user.txt` and `loot/root.txt`, and a general next phase. It never
returns the target, credential, flag or checkpoint body.

### `workspace_htb_auth_validate`

Inputs: `workspace_id`, username, a local credential reference with `source` and
`extract_after`, and a bounded timeout. Edge reads the artifact descriptor-relative,
requires exactly one extracted value, executes a fixed identity command against the
registered target and returns an opaque `hs_...` session plus UID/GID and timestamps.
The secret is absent from the model request, response, argv, environment, control
plane, Brain, Events and audit.

### `workspace_htb_command`

Inputs: workspace, session, explicit remote command and bounded timeout. The session
is bound to the runtime, workspace and locally registered target. SSH has no TTY by
default, no forwarding, no proxy, no agent forwarding and no operator key access.
Stdout and stderr are bounded. Large output is truncated and marked. Exact local
credential bytes and standalone flag-shaped values are redacted before a result can
return to the model.

### `workspace_htb_command_save`

Executes the explicit remote command and saves stdout under `loot/`, `reports/` or
`tmp/`. The write is descriptor-relative, rejects symlinks, uses `O_NOFOLLOW`, stages
with mode `0600`, syncs and replaces atomically. The result contains only relative
path, byte count, SHA-256, permissions, non-empty state and status.

### `workspace_htb_command_with_credential_stdin`

Re-extracts the session's local credential and supplies it only through stdin to one
bounded remote command, for example an authorized `sudo -S` check. It is never placed
in the schema, argv or environment and is zeroed after process completion.

### `workspace_htb_session_close`

Invalidates the runtime-bound session. Each operation already removes its one-use
askpass file; runtime cancellation additionally closes the broker, clears all
sessions and removes the private socket. Saved evidence remains local.

## Lifecycle

1. Connect the authorized VPN.
2. Register the machine once with `mcp-edge lab init`.
3. Start or continue the opaque workspace runtime.
4. Read `.mcp-devbox/instructions.md` and `.mcp-devbox/current-state.md`.
5. Call `workspace_htb_status`.
6. Validate a local credential handle and receive a session.
7. Use structured command tools to enumerate and test hypotheses.
8. Save flags and other sensitive output locally with `workspace_htb_command_save`.
9. Update the local checkpoint using handles and metadata, never values.
10. Close the session and complete cleanup.

Authorization fails closed when the workspace disappears, its mode changes, the Edge
identity changes or is revoked, `STOP` is active, the VPN route differs, the target
registration is invalid, the runtime expires or the local contract cannot be prepared.
There is no global authorization for arbitrary hosts.
