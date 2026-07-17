# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`.


Historical deployed foundations:
- P9 Brain is deployed and remains the Markdown-truth / SQLite-derived-cache foundation.
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- The historical P8.1 surface had 67 tools and reported Edge as `not_paired`; those facts remain historical evidence, not the current catalog.

Lineage:
- initial branch base: `399d7ac58842f83475c581945d0d5065a517875a`;
- Step 6 was completed at `225b6e1`;
- main was merged through `6d042c2` at `0838476`;
- latest main now contains the fail-closed GitHub Actions fallback through `77a93ad` and must be merged normally after Step 7 is committed.

Closed commits:
- `c284dcd` — Step 1: durable SQLite Operation Journal and recoverable SSE.
- `e4c674e` — Step 2: durable telemetry windows/lifetime and controller/runtime state.
- `22a9daf` — Step 3: durable digest-only console browser sessions.
- `9cdfe56` — Step 4: shared Neo-BIOS authentication firmware for console and OAuth.
- `aa1c30d` — Step 5: safe Brain node metadata, HMAC IDs and restart-safe identity.
- `225b6e1` — Step 6: persistent Events, SSE replay/reconnect, real opaque selectors, storage budget and complete live-state frontend.

Pending Step 7 closure:
- synchronize current documentation and tests to the exact 85-tool catalog;
- keep historical 67/71/78-tool evidence unchanged;
- rerun Go, frontend and security gates on the exact tree;
- commit the closure before integrating latest main.

Current catalog invariant:
- exactly 85 tools;
- `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Next: validate and commit Step 7, merge latest `origin/main` normally, rerun final gates, publish only this branch, open the PR and wait for every exact-SHA gate. Do not deploy before merge.
