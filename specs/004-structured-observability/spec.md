# Spec — P7 structured observability

Status: **active** on branch `p7-structured-observability`.

## Goal

Provide deterministic, structured operational evidence for MCP lifecycle, HTTP
transport, and JSON-RPC/tool execution without turning logs into a second audit store
or a data-exfiltration surface.

## Required behavior

1. Emit one JSON object per line with a versioned, closed schema.
2. Cover server start/stop, HTTP request completion, JSON-RPC completion, and tool-call
   outcome/duration.
3. Generate request identifiers internally; never trust or persist a client-supplied
   correlation id.
4. Record only safe dimensions: component, event, transport, normalized method/route,
   known public tool name, outcome, status code, duration, error class, build identity,
   tool count, catalog hash, and root count.
5. Never record prompts, JSON-RPC params, request/response bodies, file contents,
   command argv, audit args/files, paths, query strings, headers, tokens, OAuth
   identities, repository names, targets, domains, IP addresses, or raw error text.
6. Default to JSONL on stderr. Optional file/both modes require an administrator-owned
   path, create private permissions, enforce a bounded rotating file, and need no new
   network listener or exporter.
7. Invalid observability configuration fails startup before serving.
8. Observability failure must not corrupt JSON-RPC responses. Writer failures are
   counted internally and surfaced only as a safe startup/shutdown error class.
9. Preserve the 62-tool order, names, schemas, annotations, approvals, OAuth contract,
   public environment names outside P7, and catalog hash.

## Configuration

- `--observability` / `MCP_DEVBOX_OBSERVABILITY`: `off|stderr|file|both`; default
  `stderr`.
- `--observability-path` / `MCP_DEVBOX_OBSERVABILITY_PATH`: required by file/both;
  when omitted, defaults to `<primary-root>/.agent-memory/observability/observability.jsonl` so the dedicated directory can remain `0700` even when `.agent-memory` is broader.
- `--observability-max-bytes` / `MCP_DEVBOX_OBSERVABILITY_MAX_BYTES`: integer bytes;
  default 16 MiB; allowed range 1 MiB through 1 GiB.

## Acceptance

- Unit and integration tests prove secrets, paths, bodies, params, targets, and raw
  errors cannot enter events.
- Concurrent writes remain complete JSONL records.
- File mode uses `0700` directories, `0600` files, and one bounded `.1` rotation.
- HTTP responses expose only an opaque server-generated `X-MCP-Request-ID`.
- Local gates and GitHub CI/security workflows remain green.
- Production serves the exact P7 commit with 62 tools and the unchanged catalog hash.

## Explicit non-goals

- No Prometheus endpoint, tracing collector, OpenTelemetry exporter, external SaaS,
  dashboard, console, alert manager, or public metrics endpoint.
- No replacement or weakening of the append-only audit log.
- No prompt/source capture, profiling dump, request replay, or per-user analytics.
- No new MCP tool or application.
