# Current task

P8.1 Console 2.0 — Step 4 implementation validated locally.

## Closed predecessor retained for evidence

- P9 Brain originated on branch `p9-brain` from P8 closure commit `2e3429c9d6342e8e091cadf65293c5c85b1b3259`.
- P9 closed in production and was tagged `p9`; final merge commit: `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

## P8.1 commits

- Step 1 `c4240672c9abfbb352b7a6b8ea39d7ae0e519d22`: React/TypeScript/Vite Neo-BIOS frontend and reproducible CI/Docker build.
- Step 2 `1f97c1f`: console OAuth start/callback with digest-only state, PKCE, single-use code and strict opaque cookie while retaining bearer recovery.
- Step 3 `548da51448cf3bf0f9a5d77b4f6d94d2b0cc3b79`: query-string credentials removed; a correct `?key=` value returns HTTP 401 while Authorization bearer remains valid.

## Step 4 candidate

- Durable bounded journal under `MCP_DEVBOX_TASK_ROOT` (`/state/tasks` in the image), with server-generated IDs, operation name, fixed summary, generic controller, heartbeat and explicit states only.
- Tool calls emit best-effort journal transitions without changing the 67-tool catalog or tool schemas. Missing heartbeat is rendered disconnected; no autonomous activity is inferred.
- Authenticated `/console/tasks`, `/console/events` SSE and `/console/data` endpoints expose exact allowlisted schemas.
- Console data includes real container CPU/RAM/disk/load, MCP request/response bytes with declared `bytes / 4 (estimate)`, aggregate P7 route counters, security posture, Brain index aggregates and a bounded graph using opaque ordinal node IDs only.
- No repositories, paths, prompts, params, results, tokens, IPs, note slugs, titles, bodies, authors or provenance are exposed.
- React screens now consume real System, Agents, Tasks, Brain, Graph, Observability, Security and Events data; Edge remains honestly `Not paired`.
- Coverage gate includes taskjournal at 80%. Final measured coverage: mcpserver 83.7%, OAuth 86.1%, console 83.9%, Brain 80.9%, taskjournal 82.4%, observability 78.8%.
- Full atomic Go tests, coverage gate, vet and build pass. Frontend TypeScript, 4 Vitest tests and Vite build passed in the isolated offline runner. Local race execution is unavailable because the public runtime lacks CGO; GitHub CI owns the CGO race gate.
