# Current task

P8.1 Console 2.0 — Step 2 complete locally: first half of the OAuth migration.

## Closed predecessor retained for evidence

- P9 Brain originated on branch `p9-brain` from P8 closure commit `2e3429c9d6342e8e091cadf65293c5c85b1b3259`.
- Its release-candidate state was complete / merge-ready with the invariant **no resident service**.
- P9 subsequently closed in production and was tagged `p9`; final merge commit: `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

## P8.1 progress

- Step 1 commit `c4240672c9abfbb352b7a6b8ea39d7ae0e519d22` established the React/TypeScript/Vite Neo-BIOS frontend and reproducible CI/Docker build.
- Step 2 adds `/console/auth/start` and `/console/auth/callback` using the existing OAuth provider.
- The console client has a deterministic public client id and exact same-origin callback.
- State is stored only as a SHA-256 digest, PKCE S256 is mandatory, flows are TTL/cap bounded, and authorization codes are consumed once.
- The callback completes OAuth server-side, immediately revokes the internal token pair and creates only an opaque Secure + HttpOnly + SameSite=Strict console cookie.
- Passphrases remain accepted only by `/oauth/authorize`; codes, state, verifiers and bearers are not returned in final responses.
- Callback replay and cross-flow state/PKCE substitution are rejected.
- Static Authorization bearer and the recovery form remain available. `?key=` has deliberately not been removed yet.
- Full `go test ./...`, `go vet ./...` and `go build ./...` are green.
