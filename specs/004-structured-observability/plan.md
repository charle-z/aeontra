# Plan — P7 structured observability

Status: **active**.

1. **Threat model and schema** — define allowed fields, forbidden data, trust boundaries,
   retention, and failure posture.
2. **Safe sink** — implement concurrent JSONL output, private bounded file rotation, and
   disabled/stderr/file/both modes.
3. **Configuration** — add immutable flags/env with secure defaults and validation.
4. **MCP/HTTP instrumentation** — generate internal request ids and emit normalized
   completion events without bodies, params, paths, targets, or raw errors.
5. **Lifecycle instrumentation** — emit start/stop identity and safe configuration
   counts; remove private roots/audit paths from startup diagnostics.
6. **Adversarial tests** — prove secrets, query tokens, prompts, file paths, command
   strings, and raw failures do not appear.
7. **Operations** — document installation, mounts, permissions, rotation, updates,
   rollback, troubleshooting, and correlation with connector/Coolify incidents.
8. **Closure** — full gates, baseline, publication, deployment, exact production smoke,
   and observed GitHub Actions.

## Design rules

- Schema fields are typed and explicit; no arbitrary attributes map.
- Unknown methods/routes/errors become closed labels such as `other` or
  `internal_error`.
- Observability is not authorization, audit, billing, analytics, or source retention.
- File output is optional; stderr remains the container-native default.
- No agent-controlled runtime mutation or MCP configuration tool is introduced.
