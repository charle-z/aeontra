# Public showcase landing — design handoff

Status: **design proposal**, not implemented. Prepared on a separate branch so it does
not touch `main`. The reference mockup is `mockup.html` beside this file: it is
self-contained, opens in any browser, and is the specification.

## Why this exists

`GET /` currently returns `404 page not found`. The only public routes are `/healthz`
(one line of plain text) and `/version` (JSON). Anyone opening the domain — including a
hackathon judge — sees nothing. This is the unauthenticated public showcase already
anticipated in `docs/product-roadmap.md`, which reserved it for exactly this case:
"a separate unauthenticated public showcase may be built later only if a submission or
public demo requires it; it must remain presentation-only."

## Placement and cost

Serve it from the **existing Go binary as embedded assets**, exactly like the console in
`internal/console/assets`. Do **not** create another Coolify application, container or
listener: the production host is CPU-bound (measured p95 94 percent during a build on
two vCPU). Target resident cost is approximately zero.

Register `/` as a public unauthenticated `GET`. Do not modify `/healthz`, `/version`,
`/console`, `/mcp` or `/oauth/*`.

## Authority boundary

Presentation-only, same posture as the console:

- no tool execution, no brain queries, no repository listing;
- never expose paths, prompts, params, results, identities, IPs, audit entries or logs;
- the public 102-tool contract does not change;
- the only dynamic payload is the public runtime identity already served by `/version`
  (status, version, commit, protocol version, tool count, catalog hash);
- the policy explorer is a **local client-side simulation** built from static content.
  It performs no server call and must not imply that it does.

## Browser posture

Content-Security-Policy with no inline script — the JavaScript lives in an embedded
`app.js`. Keep the hardening headers already used by the console (nosniff, frame
denial, no-referrer, permissions policy). No CDN, remote font, analytics or third-party
resource of any kind.

## Structure

Single scrolling page, full viewport height, fixed top bar and a fixed bottom key bar
whose hint follows the visible section. Sections, in order:

1. **Thesis** — what it is, why it exists, and that much of it was written by the agent
   from a phone.
2. **Authority model** — the invariants that make it narrow rather than powerful.
3. **Policy explorer** — the interactive centrepiece. Eight requests a visitor can click
   (`cat .env`, `git push --force`, `rm -rf /etc`, `deploy now`, and two deliberately
   allowed cases). Each verdict names the invariant that produced it.
4. **Request path** — the diagram. It must render as a **graphic, not source text**.
   Generate the SVG at build time and embed it statically; if mermaid is used, apply
   colours with `classDef`, never with an `%%{init}%%` directive, which is ignored.
5. **Host and admission** — the measured capacity baseline and the admission arithmetic.
   Update with current real numbers, including the selected CPU quota.
6. **Evidence** — real, checkable moments from the project's own dated evidence,
   including failures the gates caught. Use verifiable artifacts (tags, PR numbers,
   dated baselines), never invented timestamps.
7. **Vulnerabilities** — findings resolved across both projects, as bordered panels
   rather than a wide table, with how each was detected and fixed.
8. **Live runtime identity** — the public values, plus links to `/healthz` and
   `/version` so a sceptic can verify the deployed commit independently.
9. **Honest limits** — what the project is not, stated plainly.

## Visual language

Reuse the console's VGA firmware identity so both surfaces read as one product: solid
`#0000A8` blue ground, monospace, no rounded corners, no gradients, no soft shadows.
Yellow for values, cyan for section headings, green/amber/red for state, grey chrome
bars with black text and a hard black shadow on dialogs. Single theme by deliberate
choice.

Accessibility: the boot sequence and typewriter effects must be disabled under
`prefers-reduced-motion` and skippable from the keyboard; visible focus; responsive from
320 px; wide content scrolls inside its own container so the page body never scrolls
sideways.

## Content accuracy

Every figure on the page must come from dated evidence in this repository. Do not invent
numbers, timestamps or incidents. Where something is measured but not final, say so on
the page — the honesty is part of the argument being made.
