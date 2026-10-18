# Threat model — P7 structured observability

## Assets

- MCP credentials, OAuth state, GitHub/Coolify tokens, admin grants, repository content,
  source paths, command arguments, private targets, prompts, responses, and audit data.
- Operational integrity: event order, completion outcome, build identity, and duration.

## Trust boundaries

1. Untrusted MCP client input enters HTTP/stdio JSON-RPC.
2. Repository content and tool output are untrusted data.
3. The append-only audit log is private and richer than observability.
4. Container stderr is visible to the platform operator.
5. Optional file output is visible to the filesystem/mount administrator.
6. No observability data crosses a new network boundary.

## Threats and controls

| Threat | Control |
|---|---|
| Prompt, params, response, or source copied into logs | Event type has no body/params/message/attributes field; tests inject canaries. |
| Token leaked through URL query/header/error | Route is normalized; query/header/raw error are never accepted by the logger. |
| Private path or target leaked as a label | Paths, repository, host, domain, IP, argv, and files are absent from the schema. |
| Secret-shaped value placed in a nominal label | Labels are generated from closed server constants/public tool names and defensively redacted/normalized. |
| Client forges correlation id | Server ignores client ids and generates opaque request ids internally. |
| Log injection or malformed JSONL | `encoding/json` emits one object plus one newline under a mutex. |
| File grows without bound | File mode rotates at configured bytes and retains one `.1` backup. |
| File read by other local users | Directory `0700`, active/rotated files `0600`. |
| Observability failure breaks MCP output | Emit failures never modify protocol responses; startup configuration/open failures fail before serving. |
| Metrics endpoint exposes internals | No endpoint/exporter/listener is added. |
| Observability replaces forensic audit | Audit remains separate, append-only, and policy-redacted. |

## Forbidden event data

The implementation must not add arbitrary maps or any field named/representing:

`prompt`, `body`, `params`, `arguments`, `response`, `result`, `content`, `message`,
`error`, `path`, `file`, `repo`, `repository`, `command`, `argv`, `target`, `host`,
`domain`, `ip`, `url`, `query`, `header`, `token`, `identity`, `email`, or `user`.

Only the closed `error_class` label is allowed; raw error text is forbidden.

## Residual risk

- Public tool names and coarse timing can reveal which capability class was used.
- Container/platform operators can correlate timestamps with external activity.
- File mode depends on correct administrator mount ownership and backup policy.

These are accepted because tool names are already public, timing is required for
reliability diagnosis, and no content/target/identity accompanies the event.
