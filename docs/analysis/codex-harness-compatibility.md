# Codex CLI/App Server harness compatibility

Date: 2026-08-12

Official documentation posture refreshed: 2026-08-14

## Decision

MCP Devbox can evaluate stock Codex as an optional local execution harness without forking Codex
and without giving it an OpenAI API key or a Codex subscription. The supported integration seam is
a private loopback OpenAI-compatible Responses provider, not browser automation and not reuse of a
ChatGPT browser token.

The official App Server documentation currently labels the App Server command and its WebSocket
transport experimental and unsupported for production workloads. Its initialize acceptance remains
useful compatibility evidence, but the signed product integration must not depend on App Server as
a supported production contract. The custom Responses provider is the required seam.

The pinned release is recorded in `integrations/codex/pin.json`. Host acceptance must
download that exact official asset independently, verify its SHA-256 before execution,
run with an isolated `CODEX_HOME`, and remove all model-provider credentials from its
environment.

## Verified stock seams

Codex `0.147.0` exposes all required stock seams:

- a user-defined `model_provider` with `base_url`, `wire_api = "responses"` and
  `requires_openai_auth = false`;
- `codex exec` for a bounded non-interactive vertical slice;
- experimental `codex app-server` transports over stdio, Unix socket and authenticated
  WebSocket, with initialize accepted in the pinned host test;
- built-in multiagent primitives that can later remain subordinate to MCP Devbox task,
  worktree and lease ownership.

The host acceptance in `internal/codexadapter` starts a credential-free scripted
Responses endpoint on loopback, proves that stock Codex reaches it and returns its
marker, and separately initializes the stock App Server over stdio. The test never
contacts a model API and never executes a model-generated tool.

The product adapter core is now implemented in the same package. Its official-artifact
acceptance executes a two-turn stock Codex loop: the external turn selects
`exec_command`, Codex executes the bounded tool inside its read-only test sandbox, the
tool result returns through a second durable request, and the final marker is consumed.
Codex session metadata, prompt-cache identity, encrypted reasoning and deferred
namespace/search declarations do not cross the model-turn boundary.

## Product integration shape

The production adapter should be a signed MCP Devbox process beside Codex:

```text
GPT Web model_turn_next/model_turn_respond
                 |
       durable model-turn store
                 |
 private signed Edge transport
                 |
 signed loopback Responses adapter
                 |
          stock Codex CLI
                 |
    existing workcell and brokers
```

The signed `mcp-edge codex` process now owns the private loopback listener and translates
Codex Responses requests into the existing bounded model-turn
request and translates the validated external text/tool response back to Responses
SSE. Codex retains its agent loop; MCP Devbox retains runtime identity, replay
protection, cancellation, workcell scope, GitHub brokerage and audit.

The active neutral Edge unit launches the pinned stock CLI inside the existing trusted Linux
workcell Bubblewrap boundary. It passes no OpenAI credential or ChatGPT browser state,
fixes the provider URL to the server-owned loopback listener, sets
`requires_openai_auth=false`. Managed parallel writers use server-owned worktrees,
leases and fences rather than Codex's built-in multiagent surface. OpenCode and its
provider remain only in retained signed v4 rollback releases; v5 does not ship them.

An App Server controller may be evaluated separately while it remains experimental,
but it is not a prerequisite for this adapter or the initial signed release.

## Boundaries

- Do not install Codex in the public MCP container.
- Do not pass ChatGPT cookies, browser storage, OAuth tokens or Codex account state to
  the adapter.
- Do not expose the provider beyond loopback or the private workcell namespace.
- Do not let Codex select a socket, host path, provider URL or GitHub credential.
- Do not replace the P16 workqueue with Codex session storage.
- Retain the signed v4 bridge release until the v5 real-device rollback/forward
  acceptance passes; do not ship its OpenCode components in v5.
- Implement managed worktrees and one-writer ownership before enabling writing
  multiagent workers.

## Acceptance record

The v4 Codex runtime, restart/resume, tool loop, no-duplicate-turn behavior and managed
multiworker worktrees were accepted before the v5 packaging transition. The v5 source,
release, installed-device and rollback/forward proofs must still be reported separately.
Reassess App Server independently if its official support posture changes.
