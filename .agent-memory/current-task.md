# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`.
Current HEAD after normal main integration: `ec0753d437acb781aa76392c81099394d75f0d37`.

Historical deployed foundations:
- P9 Brain is deployed and remains the Markdown-truth / SQLite-derived-cache foundation.
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- The historical P8.1 surface had 67 tools and reported Edge as `not_paired`; those facts remain historical evidence, not the current catalog.

Closed milestone commits:
- `c284dcd` — Step 1: durable SQLite Operation Journal and recoverable SSE.
- `e4c674e` — Step 2: durable telemetry windows/lifetime and controller/runtime state.
- `22a9daf` — Step 3: durable digest-only console browser sessions.
- `9cdfe56` — Step 4: shared Neo-BIOS authentication firmware for console and OAuth.
- `aa1c30d` — Step 5: safe Brain node metadata, HMAC IDs and restart-safe identity.
- `225b6e1` — Step 6: persistent Events, SSE replay/reconnect, real opaque selectors, storage budget and complete live-state frontend.
- `9c41638` — Step 7: documentation/catalog synchronization and coverage closure.
- `ec0753d` — normal merge of main through `77a93ad`, preserving the GitHub Checks→Actions fallback and fail-closed merge evidence.

Current catalog invariant:
- exactly 85 tools;
- `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Local evidence on the integrated tree:
- full Go packages pass, with resource-limited monolithic runs completed in verified groups;
- coverage gate passes, including `internal/taskjournal` at 80.8%;
- vet, build, Staticcheck, Govulncheck and Actionlint pass;
- frontend TypeScript, 5 Vitest tests and Vite production build pass;
- local race and Docker remain remote-gate requirements because this runner has no gcc or Docker backend.

Next: commit this final candidate record, rerun the exact final checks, publish only this branch, open the PR, wait every exact-SHA gate, merge by merge commit, and allow only the automatic Coolify deployment.
