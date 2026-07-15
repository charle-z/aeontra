# Latest handoff — MCP Devbox

Date: 2026-07-14
Branch: `console-2.0`
Base: deployed P9 merge `4fbe1dda02351c632e67c0f10a5c5b314df745e2`

## Current phase

P8.1 Console 2.0 is complete / merge-ready locally. Commits `c424067`,
`1f97c1f`, `548da514` and `fb66b17` deliver the React Neo-BIOS UI,
server-side console OAuth, permanent 401 for URL query credentials, header-only
recovery, durable `/state/tasks`, SSE and exact safe real-data schemas. The
catalog remains 67 tools with the P9 hash and the no resident service invariant.
P9 Brain is deployed and tagged at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Frontend check/test/build, atomic Go tests, package coverage, vet, build and diff
checks pass. Local race requires CGO and remains delegated to the blocking GitHub Race
job.

## Next safe step

Commit release-candidate docs, publish the branch, open the PR, require all exact-head
CI and security gates, merge without force, deploy only through the existing
application, verify production OAuth/cookie/query-401/recovery/task/SSE/data/catalog/
Brain behavior, then create annotated tag `p8.1`. Do not start Edge, Parrot,
HTB, web terminal or durable-agent work.
