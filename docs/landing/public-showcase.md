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

There is no second application, listener, container, framework runtime, analytics
provider, CDN, font service, database, session store, or credential. More specific
existing routes retain their handlers and contracts:

- `/mcp` remains authenticated;
- `/console` remains the authenticated operator console;
- `/oauth/*` and discovery routes remain owned by OAuth;
- `/healthz` and `/version` remain the only public runtime diagnostics.

The landing JavaScript makes exactly one same-origin request to `/version`. It requests
only the already allowlisted public runtime identity: availability, version, commit,
build time, protocol, tool count, and catalog hash. Failure renders a generic unavailable
state without forwarding raw errors.

## Visual and content structure

The page retains the design branch's square VGA/BIOS language: solid `#0000A8`, local
monospace fonts, yellow values, cyan headings, visible state colors, fixed top and bottom
bars, and no gradients or rounded design system.

The continuous document contains:

1. thesis and capability status;
2. authority model;
3. local policy explorer;
4. static request-path graphic;
5. measured host capacity and the closed P16 target-VPS acceptance;
6. dated evidence;
7. remediated vulnerability ledger;
8. live public runtime identity;
9. honest limitations and CubePath attribution.

Implemented, experimental, and planned capabilities are labeled separately. Static
claims are grounded in repository security reports, dated baselines, exact-head pull
request gates, and the accepted P16 host evidence. The landing does not reuse stale
mockup version, commit, catalog, or milestone state.

## Behavior and accessibility

- semantic header, navigation, main sections, figures, tables, lists, and footer;
- skip link and visible `:focus-visible` treatment;
- keyboard-skippable boot summary;
- `Escape` returns the internal document pane to the top;
- active-section help updates through `IntersectionObserver`;
- the policy explorer is a local deterministic simulation and performs no server call;
- typewriter output and smooth movement are disabled by `prefers-reduced-motion`;
- layouts cover 320-pixel mobile width, with wide diagrams and tables scrolling inside
  their own regions instead of forcing body-level horizontal scrolling.

## Public security boundary

The landing cannot:

- call MCP tools or proxy `/mcp`;
- approve or execute plans;
- inspect repositories, Brain, audit records, prompts, workspaces, devices, paths,
  targets, identities, or credentials;
- reuse console authentication or browser sessions;
- return private diagnostic errors.

The document CSP permits only same-origin CSS, JavaScript, images, and the one public
`/version` connection. It rejects inline script/style execution, framing, forms,
third-party resources, and base URL mutation. Responses also set `nosniff`, frame
denial, no-referrer, restrictive permissions, same-origin isolation headers, and
`no-store` caching.

## Validation contract

Tests must fail when the landing is absent or when it loses any of these properties:

- exact unauthenticated `GET /` and hardened safe 404/405 behavior;
- unchanged authentication of `/mcp` and `/console`;
- embedded local assets and exact content types;
- no inline scripts, remote asset loads, browser storage, cookies, WebSockets, SSE, or
  control-plane fetches;
- metadata and Open Graph fields;
- responsive and reduced-motion CSS;
- local-only policy simulation;
- safe `/version` identity handling;
- semantic content, capability status distinctions, real P16 acceptance, honest limits,
  and visible CubePath attribution.

Production closure requires the final pull-request HEAD to pass every applicable gate,
a merge commit into `main`, a reviewed deployment of that exact merge, and live runtime
identity matching the expected commit.
