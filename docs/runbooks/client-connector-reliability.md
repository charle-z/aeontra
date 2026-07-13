# Client and connector reliability runbook

Use this runbook when ChatGPT reports `mcp_network_error`, `Connection failed`, `Error en la transmisión de mensajes`, missing tools, lost message order, or temporary authentication loss. Do not blame the VPS, Coolify, or ChatGPT without timestamped correlation.

## Evidence record

Create one incident row before retrying anything:

| Field | Required value |
|---|---|
| Incident time | UTC timestamp and local timezone |
| Client symptom | Exact visible message; whether tool list, response stream, or authentication failed |
| MCP request/tool | Public tool name only, if known; never copy params, paths, targets, or bodies into the incident row |
| MCP request ID | Server-generated `X-MCP-Request-ID` / `request_id`, when available |
| Safe observability | Timestamp, component, event, normalized route/method, outcome, status, duration, error_class, commit/tool_count/hash |
| Deployment ID | Active/recent Coolify deployment UUID, or `none` |
| Deployment state | queued, in_progress, finished, failed, cancelled, unknown |
| `/version` commit | Exact served commit |
| `/healthz` | HTTP/result and timestamp |
| Runtime catalog | `tool_count` and `catalog_hash` from `system_runtime_info` |
| VPS snapshot | CPU, memory, swap, load, disk, I/O, Docker usage, OOM evidence |
| Coolify evidence | application status and bounded application/deployment logs |
| Conclusion | One category below, or `insufficient evidence` |

Never use a later healthy snapshot as proof that the server was healthy at the incident time. Record both timestamps.

## First-response sequence

1. Preserve the current `deployment_id`; do not trigger another deployment.
2. Query that exact deployment with `platform_deployment_status`.
3. Query `system_runtime_info` and the application status.
4. Run `cmd/mcp-catalog-smoke` against production with the commit that should be live.
5. Correlate the server-generated request id with content-free observability events. Use
   only normalized fields; do not copy request bodies, params, paths, targets, headers,
   tokens, identities, or raw errors into incident records.
6. If a deployment is active, retry the same deployment status after a bounded interval. Do not create a deploy loop.
7. Only consider another deployment after the prior deployment is terminal and the served commit, health, and Coolify state prove it is necessary.
8. If the client shows fewer tools than `system_runtime_info`, treat it as a client catalog/cache discrepancy; do not redeploy solely for that symptom.

## Classification matrix

### 1. Expected MCP restart during self-deployment

Classify as an expected restart only when all of these correlate:

- a known deployment was `in_progress` at the symptom time;
- application health or the MCP transport dropped temporarily;
- Coolify replaced/restarted the container;
- the same deployment finishes successfully;
- `/version` then reports the new expected commit;
- health returns and the catalog remains 62 tools with the expected hash.

A brief connector error during that interval is expected transport fallout, not proof of a client defect or VPS saturation. Preserve the deployment ID and avoid triggering a second build.

### 2. VPS saturation or resource exhaustion

Classify as VPS saturation only when contemporaneous host evidence supports it. Capture through the VPS/Coolify operator surface:

```text
uptime
free -h
swapon --show
vmstat 1 5
df -h
df -i
iostat -xz 1 5
docker system df
docker stats --no-stream
journalctl -k --since '<incident-start>' --until '<incident-end>' | grep -Ei 'oom|out of memory|killed process'
```

Record:

- CPU utilization and steal time;
- memory available, swap usage, and paging;
- 1/5/15-minute load average relative to vCPU count;
- filesystem and inode usage;
- I/O wait, latency, queue depth, and device saturation;
- Docker image/build-cache/volume usage;
- build start/end timestamps and duration;
- kernel or container OOM-kill messages.

Strong evidence includes OOM kills, sustained zero/near-zero available memory with paging, disk/inode exhaustion, sustained load far above available CPU with long run queues, or saturated I/O aligned with the failures. A slow build alone is not proof.

### 3. Tool timeout or MCP operation failure

Classify as a tool timeout/error when:

- the MCP server remains healthy and serves the same commit;
- one named operation exceeds its configured timeout or returns an explicit bounded error;
- content-free observability identifies the public tool, request id, timestamps,
  duration, outcome, and closed error class;
- unrelated tools continue to work.

Record the public operation, configured timeout, observed duration, closed error class,
and whether the server cancelled the work. Keep params, paths, targets, source, results,
tokens, identities, and raw errors in the separately authorized private evidence path;
do not copy them into observability or this incident table. Distinguish policy denial,
approval required, subprocess timeout, upstream API timeout, and malformed response.
Do not redeploy for a reproducible input/policy error.

### 4. Coolify/build/deployment failure

Classify as Coolify when the platform evidence is terminal and negative:

- deployment state is `failed` or `cancelled` unexpectedly;
- builder logs show checkout, build, image, healthcheck, proxy, or container-start failure;
- the application remains unhealthy or keeps serving the old commit after the failed deployment;
- the error is reproducible in the same deployment logs.

Capture the deployment UUID, expected commit, failing stage, first causal error, final status, application health, and whether rollback/old container remained available. Do not infer a Coolify failure merely because ChatGPT lost a message while the deployment finished successfully.

### 5. ChatGPT client or transport presentation problem

Classify as client/transport only when server-side evidence stays stable across the symptom:

- no deployment or container restart overlaps the incident;
- `/healthz`, `/version`, and `system_runtime_info` remain stable;
- Coolify reports the application healthy;
- VPS metrics show no correlated exhaustion;
- MCP audit/logs show either a completed response or no corresponding request;
- the UI loses, duplicates, reorders, or stops displaying messages/tools anyway.

This category includes stale/incomplete client tool catalogs when the server reports the full 62-tool catalog and expected hash. The server cannot prove the exact internal cause inside the ChatGPT client; record it as client/transport presentation evidence, not as an asserted OpenAI root cause.

## Decision examples

| Observation | Classification |
|---|---|
| Connector drops while deployment `bn...` is active; health returns with new commit | Expected restart |
| Kernel logs show OOM kill during Docker build; swap and memory exhausted | VPS saturation/OOM |
| `project_validation_execute` hits its fixed timeout while health stays green | Tool timeout |
| Deployment terminal `failed`; builder log shows Dockerfile command failure | Coolify/build failure |
| UI loses sequence; server commit/health/catalog and VPS remain stable; no restart | Client/transport presentation problem |
| Evidence conflicts or timestamps do not overlap | Insufficient evidence; keep investigating |

## Safe recovery

- **Expected restart:** wait only through bounded status checks of the same deployment, then re-run catalog smoke.
- **VPS saturation:** stop duplicate builds, free safe unused cache only with operator approval, resize/tune resources if evidence justifies it, and retry once after recovery.
- **Tool timeout:** reduce the bounded workload or fix the tool/profile timeout deliberately; do not introduce a free shell or unbounded timeout.
- **Coolify failure:** fix the causal build/configuration error, verify the old production commit, then create one reviewed deployment plan.
- **Client/transport:** reconnect/refresh the client and compare its catalog with `system_runtime_info`; do not delete OAuth configuration or redeploy a healthy server as the first response.

## Current verified example — Step 90

Deployment `bn9ehyy686ag4zm5os5cijxl` was initially last observed `in_progress`. On 2026-07-13 it was re-queried and found `finished`, serving commit `112ca8ce06ffdeba570e486a548801ee21692a6f`. Production health and catalog smoke passed with 62 tools and the expected hash. Therefore no second deployment was started. Any connector interruption during that deployment window can be labeled an expected restart only if its timestamp overlaps the deployment; later UI problems require separate evidence.
