# Console Durable Live State & Auth Firmware

Branch: `console-durable-live-state`, based on `origin/main` merge `399d7ac`.

Completed commits:
- `c284dcd` — Step 1: durable SQLite Operation Journal and recoverable SSE.
- `e4c674e` — Step 2: durable telemetry windows/lifetime and controller/runtime state.
- `22a9daf` — Step 3: durable digest-only console browser sessions.

Step 4 validated and ready to commit:
- `/console` and `/oauth/authorize` use one embedded `/auth/assets/firmware.css` asset;
- VGA/Neo-BIOS visual language, square borders, no gradients or external resources;
- exact auth CSP: `default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'`;
- no inline style, script or event handlers remain in either auth page;
- forms operate without JavaScript, with labels, focus-visible, mobile layout and reduced-motion support;
- OAuth shows safe client/scope/resource labels and visible ready/denied/locked states while hidden values are server-revalidated;
- scope is allowlisted to `mcp`.

No merge or deployment. Catalog must remain 78 tools and the required hash unchanged.
