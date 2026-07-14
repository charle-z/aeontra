# Current task

P8.1 Console 2.0 — Step 1: React/Vite Neo-BIOS frontend foundation.

## Closed predecessor retained for evidence

- P9 Brain originated on branch `p9-brain` from P8 closure commit `2e3429c9d6342e8e091cadf65293c5c85b1b3259`.
- Its release-candidate state was complete / merge-ready with the invariant **no resident service**.
- P9 subsequently closed in production and was tagged `p9`; final merge commit: `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

## Active Step 1

- Branch `console-2.0` was created exactly from the closed P9 merge commit.
- Design handoff imported from `origin/console-2.0-neo-bios-design` under `docs/console-2.0/`.
- Frontend source lives under `web/console` using React, TypeScript and Vite. Astro was intentionally not selected because the console is a coordinated interactive application rather than a mostly-static content site.
- The current shell renders the post-P9 `/console/status` values and marks unavailable backend data honestly; no mock VPS, task, Brain, observability or device metrics are rendered.
- CI and Docker are being wired to check, test and build the frontend before Go tests/builds.
- `pnpm-lock.yaml` was generated through the private fixed validation runner. The full `pnpm-validate` execution was blocked by the platform invoker before reaching the runner; source security tests pass and remote CI will be used as an independent frontend build gate if the invoker remains blocked.
- No production deployment, OAuth change, query-key removal or catalog change has occurred in Step 1.
