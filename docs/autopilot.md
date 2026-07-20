# Durable local autopilot

P15 exposes only lifecycle controls to the exterior MCP client. Lab commands,
credentials, flags, checkpoints, target values and command output stay on the paired
Edge. `workspace_autopilot_start` accepts only an authorized workspace ID and
`completed_or_cancelled`; status, pause, resume and cancel use only that opaque ID.

The Edge stores `.mcp-devbox/autopilot-state.json` atomically with mode `0600` and
supervises `mcp-autopilot-worker` from the currently verified signed bundle. Each
worker process performs one bounded model/action/checkpoint cycle and exits. The
supervisor starts the next cycle while state remains `running`, including after an
Edge restart or server redeploy. There is no fixed job lifetime.

The local model configuration is `/etc/mcp-devbox/autopilot-model.json`, a Debian
conffile preserved across updates. Providers implement `LocalAgentModel`; P15 ships
a loopback-only HTTP adapter usable by a local/self-hosted service or a local
OpenCode adapter, plus a deterministic test provider. Redirects, non-loopback
hosts, arbitrary provider commands and per-job provider configuration are rejected.

Only these action kinds reach the local broker: `status`, `auth_validate`,
`command`, `command_save`, `command_with_credential_stdin`, `session_close`,
`artifact_metadata`, `checkpoint_update`, `finish`, and `block`. Broker requests use
a private Unix socket and the registered target/VPN/authorization revision. The
signed outbound control channel receives only opaque job/workspace IDs, state,
progress revision, cycle count and a closed safe code.

Circuit breakers block new cycles after two no-progress cycles, a repeated action
without new evidence, three identical failures, authorization or VPN loss, `STOP`,
provider refusal, invalid contract/observation, or the local storage limit. Resume
keeps evidence and checkpoint but resets transient breaker counters.
