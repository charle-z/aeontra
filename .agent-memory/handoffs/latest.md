# Latest handoff — MCP Devbox

Date: 2026-07-13
Branch: `p7-structured-observability`
Deployed base: `main` at `ab0cf153fe898784dac6d48a062de78abb4d5f5d`

## Current phase

P7 structured observability is active. It is separate from the private append-only
audit log and has a closed content-free JSONL schema.

## Local implementation

- Safe sink, private one-backup rotation, config flags/env, lifecycle/HTTP/RPC/tool
  instrumentation, internal request IDs, failure count, adversarial tests, and operator
  documentation are implemented.
- No tool, endpoint, exporter, external collector, dashboard, console, OAuth change,
  or new application is introduced.
- Full suite currently passes; observability coverage baseline is 74.4% with a 70% gate.

## Security boundary

Never add prompts, bodies, params, results, source, paths, repos, commands, targets,
URLs, queries, headers, tokens, identities, IPs/domains, raw errors, or arbitrary
attribute maps to observability. Known public tool names and coarse durations are the
only capability-level dimensions.

## Next safe step

Complete documentation tests and all gates, remove temporary files, commit/publish P7, observe
main Actions, deploy the existing Coolify application, verify exact runtime identity
and safe JSONL logs, then create the P7 closure baseline. Start the console only on a
new branch/spec after P7 closes.
