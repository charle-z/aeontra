# Promptless workspace runtime continuation

A workspace is persistent local state owned by one paired Edge device. A runtime is
an ephemeral execution lease created for one explicit continuation request. Removing
or finishing a runtime does not remove the workspace, its local contract, its
checkpoint, or its evidence.

This is the primary interactive mode for an authorized MCP client such as ChatGPT.
While the client conversation remains active, the client drives each Edge request with
`model_turn_next` and `model_turn_respond`. OpenCode is only the pinned local execution
harness in this path; it is not the model or model provider and receives no OpenAI or
ChatGPT credential or browser state. The P15 loopback autopilot provider is optional
and is used only when execution must continue without an active client conversation.
No daemon drives the client UI or creates a replacement conversation automatically.

## Public MCP contract

`workspace_runtime_continue` accepts exactly:

```json
{
  "workspace_id": "ws_00000000000000000000000000000000",
  "timeout_seconds": 3600,
  "idempotency_key": "continue-20260720T230000Z-a1b2c3d4"
}
```

The schema is closed. It has no objective, prompt, instructions, command, target,
host, IP address, machine, platform, credential, secret, flag, checkpoint, path,
shell, environment, or generic options field.

The caller generates one fresh `idempotency_key` for every explicit continuation and
reuses it only when retrying that same call. An accidental replay with that key returns
the same runtime even if the MCP transport reuses JSON-RPC IDs across chats. A second
explicit request while that workspace already has an
active continuation also returns the active runtime instead of creating a duplicate.
Each accepted call therefore creates at most one runtime.
After the runtime reaches a terminal state, a later explicit request may create one
new runtime. A failed or expired runtime is never retried automatically.

## Lease recovery

Before the first model turn, the control plane serves the server-owned objective to
the paired Edge through a signed, receipt-bound lease. If that private objective body
is temporarily unavailable, the server returns a retryable service-unavailable result
and restores the runtime to `awaiting_edge`; it does not report an Edge failure or
create a terminal runtime. The Edge retries with the same opaque lease receipt. A
runtime becomes terminal only through its normal explicit completion, cancellation,
failure, or expiry transitions.

The safe public response contains only:

```text
runtime_id
device_id
workspace_id
controller
state
last_sequence
updated_at
result_ref (terminal success only)
phases (bounded safe startup timeline)
```

Each phase contains only a closed phase name, server-owned timestamps, derived
non-negative durations, and an optional closed retry category and bounded count.
No objective, prompt, tool body, command, local path, checkpoint, credential, or
private error text is part of the public timeline.

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

The Edge remains the source of truth. Inside the sandbox, the runtime reads the private
control mount:

```text
/workspace/.mcp-devbox/instructions.md
/workspace/.mcp-devbox/current-state.md
```

These files are stored under the Edge's per-workspace runtime root rather than in the
source checkout. A legacy source-side `.mcp-devbox` directory is retained only for
backward compatibility and is not used for new workcell control state.

For `htb-linux`, the local workspace registry and contract continue to enforce the
immutable target, VPN preflight, target-locked broker, checkpoint redaction,
local-only secret handling, and `--save-output` for sensitive artifacts.

Registered `linux-workcell` workspaces in either `dev` or `htb-linux` mode may use
this continuation path. Caller-supplied operational goals remain forbidden in both
modes. In `htb-linux`, the private Edge provider offers only the structured HTB
actions authorized by the local contract; raw credential material remains local.

## Normal onboarding and continuation

```text
1. Connect the VPN.
2. Run mcp-edge lab init once for the machine.
3. Ask the chat to continue the registered workspace.
4. The chat calls workspace_runtime_continue using the workspace id, timeout and a
   fresh opaque idempotency key generated by the chat.
5. The chat polls `model_turn_next` and answers each pending turn with
   `model_turn_respond` until the runtime reaches a terminal state or the user stops.
6. The Edge executes the local trusted contract and its structured tools.
```

## Model-turn completion gate

Every current `model_turn_respond` caller should declare one closed `task_state`:

- `active` is valid only with `finish_reason=tool_calls` and at least one tool id
  offered by the current request. Progress text may accompany the call, but cannot
  replace the next executable action.
- `blocked` is valid only with `finish_reason=error` or `cancelled` and no tool calls.
- `complete` is valid only with `finish_reason=stop`, no tool calls and no
  sentence-leading declaration of pending work such as `Now I will...`,
  `The next step is...`, `Ahora voy a...` or `Queda por comprobar...`.

`finish_reason=length` is incomplete and is rejected. A rejected response does not
consume the durable turn, so the active MCP client can submit a corrected response.
For compatibility with clients that cached the earlier tool schema, an omitted
`task_state` is inferred from `finish_reason`: `tool_calls` maps to `active`, `stop`
maps to `complete`, and `error`/`cancelled` map to `blocked`. The inferred state crosses
the same validation gate; omission does not bypass pending-action or tool-id checks.
The same validation runs again in the stock Codex loopback adapter before a durable
response becomes a Responses API event. `task_state` is MCP admission metadata and is
not added to the strict durable provider payload, preserving compatibility with signed
Edge releases that implement the prior payload shape.

This gate applies to managed Codex/model-turn runtimes. MCP Devbox cannot intercept a
direct ChatGPT client response that closes without calling an MCP tool. Durable tasks,
workspaces and checkpoints remain the recovery boundary for that client-controlled
case.

`lab init` prepares and registers the persistent local workspace. It is not repeated
for every runtime. `workspace_runtime_continue` is for later ephemeral executions.

A future signed bootstrap that removes the remaining local `lab init` step is a
separate feature. This continuation tool does not install or update Edge, create a
workspace, connect a VPN, or bootstrap an unregistered machine.
