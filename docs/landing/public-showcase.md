# Public landing implementation contract

Status: **implemented in source; deployment is accepted only when production reports the exact merged commit.**

## Design source

The read-only source of truth was remote branch `landing-public-showcase-design`:

- `docs/landing/design-public-showcase.md`
- `docs/landing/mockup.html`

The mockup is a visual specification, not production source. It contained intentionally
static demo identity, outdated milestone claims, inline JavaScript, and Mermaid source.
Those elements are not copied into the runtime.

## Production architecture

The result is a presentation-only surface. It is Hosted on CubePath with the existing
Coolify application, but it is not a public control plane.

The landing is implemented by package `internal/landing` and mounted at exact public
route `GET /` by the existing Go HTTP server. The same binary embeds:

- `assets/index.html`
- `assets/app.css`
- `assets/app.js`
- `assets/request-path.svg`
- `assets/social-card.svg`

The canonical Pixelgrama presentation evidence is
`docs/showcase/pixelgrama-evidence.json`. The build validates and embeds those exact
bytes, then serves them as the static public resource
`/showcase/pixelgrama-evidence.json`. Missing or invalid evidence fails closed at
startup instead of producing a partial presentation.

There is no second application, listener, container, framework runtime, analytics
provider, CDN, font service, database, session store, or credential. More specific
existing routes retain their handlers and contracts:

- `/mcp` remains authenticated;
- `/console` remains the authenticated operator console;
- `/oauth/*` and discovery routes remain owned by OAuth;
- `/healthz` and `/version` remain the only public runtime diagnostics.

The landing JavaScript makes exactly two same-origin public requests. `/version`
returns the already allowlisted runtime identity: availability, version, commit, build
time, protocol, tool count, and catalog hash. The static
`/showcase/pixelgrama-evidence.json` resource supplies the guided Pixelgrama story from
the same validated bytes embedded at startup. Neither request reaches GitHub or a
private control-plane route. Failure renders a generic unavailable state without
forwarding raw errors.

## Visual and content structure

The page retains the design branch's square VGA/BIOS language: solid `#0000A8`, local
monospace fonts, yellow values, cyan headings, visible state colors, fixed top and bottom
bars, and no gradients or rounded design system.

The continuous document contains:

1. a benefit-first hero that states the excessive-authority problem, the
   bounded-tool solution, the three autonomy choices, and the public Pixelgrama proof
   before component detail;
2. exactly three primary hero actions: the canonical proof, the authority model,
   and the repository;
3. capability status below the hero;
4. a visual comparison between broad ambient authority and Aeontra's explicit
   bounded authority, followed by the conceptual `read-only`, `ask`, and `allow`
   selector;
5. a six-step, read-only Pixelgrama walkthrough generated from the canonical manifest;
6. local policy explorer;
7. static request-path graphic;
8. measured host capacity and the closed P16 target-VPS acceptance;
9. dated evidence;
10. remediated vulnerability ledger;
11. live public runtime identity;
12. honest limitations and CubePath attribution.

Implemented, experimental, and planned capabilities are labeled separately. Static
claims are grounded in repository security reports, dated baselines, exact-head pull
request gates, and the accepted P16 host evidence. The landing does not reuse stale
mockup version, commit, catalog, or milestone state.

## Behavior and accessibility

- semantic header, navigation, main sections, figures, tables, lists, and footer;
- skip link and visible `:focus-visible` treatment;
- keyboard-skippable boot summary exposed as a modal dialog while visible, with the
  document surface inert and hidden from assistive technology until dismissal;
- `Escape` returns the internal document pane to the top;
- active-section help updates through `IntersectionObserver`;
- the policy explorer is a local deterministic simulation and performs no server call;
- the authority-mode selector is also local-only, uses tabs and panels with
  arrow/Home/End keyboard navigation, and never changes real policy;
- the guided demo reads the embedded manifest once, validates its public schema in the
  browser, builds all records with `textContent` and DOM nodes, and fails to a generic
  unavailable state; its section reports loading through `aria-busy`;
- the walkthrough distinguishes historical PR heads from the production commit, keeps
  the exact historical policy mode and tool list marked as unpublished, and links each
  PR's public changed-files view rather than copying file details into a second source;
- typewriter output and smooth movement are disabled by `prefers-reduced-motion`;
- layouts cover 320-pixel mobile width, with wide diagrams and tables scrolling inside
  their own regions instead of forcing body-level horizontal scrolling.

## Public security boundary

The landing cannot:

- call MCP tools or proxy `/mcp`;
- approve or execute plans;
- turn the guided demo into an operational client, request grants, or infer unpublished
  policy modes, tool lists, plan IDs, approvals, or audit records;
- inspect repositories, Brain, audit records, prompts, workspaces, devices, paths,
  targets, identities, or credentials;
- reuse console authentication or browser sessions;
- return private diagnostic errors.

The document CSP permits only same-origin CSS, JavaScript, images, `/version`, and the
embedded evidence resource. It explicitly rejects inline script/style attributes,
objects, framing, forms, third-party resources, and base URL mutation. Responses also set `nosniff`, frame
denial, no-referrer, restrictive permissions, same-origin isolation headers, and
`no-store` caching.

## Validation contract

Tests must fail when the landing is absent or when it loses any of these properties:

- exact unauthenticated `GET /` and hardened safe 404/405 behavior;
- unchanged authentication of `/mcp` and `/console`;
- embedded local assets and exact content types;
- closed, valid, embedded Pixelgrama evidence with safe startup failure;
- no inline scripts, remote asset loads, browser storage, cookies, WebSockets, SSE, or
  control-plane fetches;
- metadata and Open Graph fields;
- responsive and reduced-motion CSS;
- local-only policy simulation;
- one validated same-origin manifest fetch for a six-step request, perimeter, change,
  validation, external-operations, and production-result walkthrough;
- explicit separation of historical PR SHAs from the currently observed production
  commit, with unavailable historical mode and tool detail left unavailable;
- safe `/version` identity handling;
- a bilingual benefit-first hero with problem, solution, autonomy, Pixelgrama
  proof, and exactly three primary actions;
- a bilingual, mobile-readable authority comparison that does not depend only on
  color, describes `allow` as configured autonomy, avoids presenting `ask` as the only
  safe mode, and states that reduced authority is not absolute safety;
- an accessible three-tab conceptual mode selector whose interaction remains entirely
  in the browser;
- semantic content, capability status distinctions, real P16 acceptance, honest limits,
  and visible CubePath attribution.

Production closure requires the final pull-request HEAD to pass every applicable gate,
a merge commit into `main`, a reviewed deployment of that exact merge, and live runtime
identity matching the expected commit.
