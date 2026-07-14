# Current task

P8.1 Console 2.0 — Step 3 candidate: query-string credentials removed.

## Closed predecessor retained for evidence

- P9 Brain originated on branch `p9-brain` from P8 closure commit `2e3429c9d6342e8e091cadf65293c5c85b1b3259`.
- Its release-candidate state was complete / merge-ready with the invariant **no resident service**.
- P9 subsequently closed in production and was tagged `p9`; final merge commit: `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

## P8.1 progress

- Step 1 `c4240672c9abfbb352b7a6b8ea39d7ae0e519d22`: React/TypeScript/Vite Neo-BIOS frontend and reproducible CI/Docker build.
- Step 2 `1f97c1f`: console OAuth start/callback, digest-only state, PKCE, single-use code and strict opaque cookie while retaining bearer recovery.
- Step 3 changes `authOK` to accept only `Authorization: Bearer`; URL query values are ignored.
- The mandatory regression test proves `?key=<valid MCP_DEVBOX_TOKEN>` returns HTTP 401, while the same token in the Authorization header remains valid.
- OAuth MCP authorization and the console OAuth flow remain green.
- Operator docs now direct remote clients to clean `/mcp` OAuth and describe bearer as header-only recovery.
- Frontend check, 3 Vitest tests and Vite build are green after displaying query-key auth as removed.
