# Promptless workspace runtime continuation

A workspace is persistent local state owned by one paired Edge device. A runtime is
an ephemeral execution lease created for one explicit continuation request. Removing
or finishing a runtime does not remove the workspace, its local contract, its
checkpoint, or its evidence.

## Public MCP contract

`workspace_runtime_continue` accepts exactly:

```json
{
  "workspace_id": "ws_00000000000000000000000000000000",
  "timeout_seconds": 3600
}
```

The schema is closed. It has no objective, prompt, instructions, command, target,
host, IP address, machine, platform, credential, secret, flag, checkpoint, path,
shell, environment, or generic options field.

Each JSON-RPC request is idempotent. An accidental replay of the same request returns
the same runtime. A second explicit request while that workspace already has an
active continuation also returns the active runtime instead of creating a duplicate.
Each accepted call therefore creates at most one runtime.
After the runtime reaches a terminal state, a later explicit request may create one
new runtime. A failed or expired runtime is never retried automatically.

The safe public response contains only:

```text
runtime_id
workspace_id
device_id
state
created_at
expires_at
last_sequence
failure_category
```

## Server-owned objective

The caller cannot choose or extend the runtime objective. The server uses the
versioned objective `resume-local-contract-v1`:

```text
Resume the registered workspace using its local trusted contract and persistent checkpoint. Perform only operations authorized by the local contract. Keep local-only values local. Return a bounded safe status.
```

The objective contains no target, machine, credential, flag, checkpoint content, or
caller-provided operational instruction.

## Workspace ownership

The Edge periodically publishes a signed, replacement snapshot containing only each
workspace's opaque ID, profile, and mode. It never publishes the local path, target,
machine metadata, VPN interface, credential handles, evidence, instructions, or
checkpoint. The control plane accepts a continuation only when the workspace resolves
to an active paired device and the profile/mode combination is recognized.

The Edge remains the source of truth. At runtime it reads:

```text
.mcp-devbox/instructions.md
.mcp-devbox/current-state.md
```

For `htb-linux`, the local workspace registry and contract continue to enforce the
immutable target, VPN preflight, target-locked broker, checkpoint redaction,
local-only secret handling, and `--save-output` for sensitive artifacts.

## Normal onboarding and continuation

```text
1. Connect the VPN.
2. Run mcp-edge lab init once for the machine.
3. Ask the chat to continue the registered workspace.
4. The chat calls workspace_runtime_continue using only workspace_id and timeout_seconds.
5. The Edge executes the local trusted contract.
```

`lab init` prepares and registers the persistent local workspace. It is not repeated
for every runtime. `workspace_runtime_continue` is for later ephemeral executions.

A future signed bootstrap that removes the remaining local `lab init` step is a
separate feature. This continuation tool does not install or update Edge, create a
workspace, connect a VPN, or bootstrap an unregistered machine.
