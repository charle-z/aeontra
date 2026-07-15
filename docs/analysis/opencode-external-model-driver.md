# OpenCode external model driver analysis

## Pin

This bridge targets only:

- OpenCode `v1.18.1`, commit `99f638d8293f6985726ba509da602296c4963497`.
- `@ai-sdk/provider` `3.0.8`, source tag `@ai-sdk/provider@3.0.8`, source commit `0808471734`.

The machine-readable pin is `integrations/opencode/pin.json`. Upgrading either side
requires rerunning the compatibility tests and the isolated vertical slice; floating
versions are not accepted.

Primary source anchors:

- OpenCode provider loader:
  `packages/opencode/src/provider/provider.ts` at the pinned commit.
- OpenCode session runtime:
  `packages/opencode/src/session/llm.ts` and
  `packages/opencode/src/session/llm/ai-sdk.ts` at the pinned commit.
- AI SDK provider contract:
  `packages/provider/src/language-model/v3/` at the pinned AI SDK tag.

## Provider package and loading

OpenCode resolves the configured model's `api.npm`. Bundled packages use an internal
loader; any other package is installed or, when the value starts with `file://`,
imported directly. It selects the first module export whose name begins with
`create`, invokes it with `{ name, ...options }`, and later calls
`sdk.languageModel(model.api.id)`.

Therefore the bounded integration is a local ESM package exporting
`createMCPDevboxModelBridge(options)`. It returns an object with
`languageModel(modelID)` and does not expose chat, responses, credentials, or a
provider fallback. The OpenCode config references the package through an exact
`file://` URL.

## Runtime call path

The default OpenCode session path always invokes AI SDK `streamText`, wraps the
selected `LanguageModelV3`, supplies the transformed prompt, active tools,
`toolChoice`, generation parameters, headers and an `AbortSignal`, then converts the
AI SDK `fullStream` into OpenCode `LLMEvent` values. The provider must consequently
implement both `doGenerate` and `doStream`; implementing only non-streaming generation
is not compatible even though the external rendezvous initially returns one complete
response.

The first slice implements `doStream` as a one-response stream rather than token
streaming. It waits for one durable external response and emits a bounded sequence of
AI SDK v3 stream parts. `doGenerate` uses the same turn protocol and returns the
corresponding complete result.

## Request mapping

`LanguageModelV3CallOptions` supplies:

- `prompt`: system, user, assistant and tool messages;
- function tools: name, description and exact input JSON schema;
- tool choice;
- maximum output tokens and sampling parameters;
- response format;
- provider options and headers;
- `abortSignal`.

The provider canonicalizes only those supported fields into the model-turn request.
Each offered function tool receives a stable request-local ID derived from its exact
name and schema. Provider tools and multimodal URL/file payloads are rejected in the
initial slice rather than silently transformed or sent elsewhere.

Tool results already executed by OpenCode arrive in subsequent prompt messages as
`tool-result` parts, including call ID, tool name and typed output. They are preserved
inside the canonical request so the external model can continue the agent loop.

## Response mapping

The bounded model response supports:

- text;
- client-executed function tool calls;
- finish reason;
- usage;
- cancellation and explicit error.

For `doGenerate`, text becomes `LanguageModelV3Text`; tool calls become
`LanguageModelV3ToolCall` with stringified JSON input and `providerExecuted=false`.
For `doStream`, the provider emits:

1. `stream-start`;
2. optional `text-start`, one `text-delta`, and `text-end`;
3. one `tool-call` part per validated call;
4. `finish` with v3 usage and finish reason.

The external finish reasons map as follows:

| External | AI SDK v3 unified |
|---|---|
| `stop` | `stop` |
| `tool_calls` | `tool-calls` |
| `length` | `length` |
| `cancelled` | `other` with raw `cancelled` |
| `error` | `error` |

Unknown reasons are rejected before persistence. Token usage is optional; absent
values remain `undefined` rather than being fabricated.

## Tool execution and loop compatibility

OpenCode's AI SDK runtime receives `tool-call` stream events, executes the matching
OpenCode tool through its existing tool registry, emits tool result/error events, and
adds the resulting parts to the next model prompt. The bridge therefore does not
execute shell, filesystem, LSP, formatter or MCP tools itself. It only exposes the
exact tools offered by OpenCode and accepts responses referencing those offered IDs.
This preserves OpenCode's permission checks, sessions, context, tool execution and
agent loop.

## Cancellation

OpenCode passes an `AbortSignal` to the provider. The bridge registers a one-shot
listener before creating or waiting for a turn. Aborting calls the transport's
`Cancel(turnID)` when a turn exists, aborts the local wait immediately, removes the
listener, and never retries through another provider.

## Streaming

True token streaming is deliberately out of scope for this vertical slice. The
provider still satisfies the streaming interface by emitting a complete bounded
response through a `ReadableStream`. This is compatible with OpenCode's `fullStream`
adapter and leaves room for future incremental response chunks without changing the
durable rendezvous identity.

## Errors

Input incompatibility, persistence failure, digest/sequence mismatch, late response,
replay, cancellation and malformed external response are surfaced as provider errors.
`doStream` errors before stream creation reject the call; errors after stream creation
are emitted as an AI SDK `error` part and the stream closes. There is no automatic
fallback to another model, API or local inference runtime.

## Security and network posture

The provider contains no API key field, does not read provider credential environment
variables, and does not call OpenAI, Anthropic, OpenRouter, Codex or any model API. Its
only transport is the local model-turn driver configured for loopback. Tests install a
network-denying dispatcher and fail on every non-loopback request. The OpenCode model
ID is a local routing label, not a real model name.

## Compatibility decision

A custom provider is viable at the pinned versions. The critical requirements are:

- implement AI SDK `LanguageModelV3`, especially `doStream`;
- use a local `file://` provider package with a `create...` export;
- emit AI SDK v3 tool calls so OpenCode, not the provider, executes tools;
- preserve tool result prompt parts on following turns;
- propagate the exact abort signal;
- fail closed on unsupported prompt content and never select another provider.
