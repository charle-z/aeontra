# Current task

P8.1 Console 2.0 is complete / merge-ready locally on branch `console-2.0`, based exactly on deployed P9 merge `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Implementation commits:

- `c4240672c9abfbb352b7a6b8ea39d7ae0e519d22` — React/TypeScript/Vite Neo-BIOS foundation.
- `1f97c1f7c077752a435bdecd484229f0381dc306` — console OAuth state/PKCE/callback/cookie flow.
- `548da51448cf3bf0f9a5d77b4f6d94d2b0cc3b79` — URL query credentials removed; correct legacy query value returns 401 while Authorization recovery remains valid.
- `fb66b175cee23b0211f307fe50bf5ae73a6bbd74` — durable `/state/tasks`, SSE and exact real-data console schemas.

Frontend check/test/build, atomic Go tests, package coverage, vet, build and diff checks pass. The catalog remains 67 tools with P9 hash `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`. Local race cannot run without CGO; the blocking GitHub Race job remains mandatory.

Next: commit release-candidate docs, publish `console-2.0`, open the PR, require exact-head CI/security gates, merge without force, deploy through the existing Coolify application, verify production OAuth/cookie/query-401/recovery/task/SSE/data/catalog/Brain behavior, and create annotated tag `p8.1`. Edge, Parrot, HTB, autonomous agents and a web terminal remain outside scope.
