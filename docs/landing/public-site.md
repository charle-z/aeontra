# Aeontra public product site

Status: **implemented in source**. Production closure still requires exact-head CI,
merge, isolated managed deployment, DNS/TLS validation and live identity verification.

## Purpose

The package under `internal/landing` owns an open-source product site at the exact
public route `GET /`. It explains Aeontra's current software boundary, links to the
public alpha and source, and exposes a bounded live control-plane identity.

The same embedded assets remain available on an Aeontra control plane. The recommended
marketing-domain deployment uses the separate `aeontra-site` executable and
`Dockerfile.site`. That process serves only the landing, `/healthz`, `/readyz`, and a
strict sanitized proxy of one administrator-configured public `/version` endpoint. It
keeps a marketing domain outside the MCP/OAuth/console trust boundary.

The site does not act as an MCP client. It cannot call tools, approve plans, inspect
repositories, read audit data, reuse console sessions or obtain credentials. In the
isolated deployment, `/mcp`, OAuth and `/console` do not exist. In a control-plane
deployment, those routes retain their existing authentication and ownership.

## Brand and content direction

The page uses a typographic Aeontra wordmark while no standalone logo exists. Its
visual system is an editorial operations sheet: warm paper, black structure, one blue
authority field and a chartreuse signal color. It uses square geometry, visible rules,
system typefaces and direct product language. It includes no generated illustration,
remote font, stock image, animation-led intro, gradient, analytics or tracking tag.
There are no analytics.

Current content is organized around:

1. a concrete product statement and representative request-to-receipt path;
2. a bounded execution trace;
3. repository, delivery, durable-work and private-Edge surfaces;
4. the `read-only`, `ask`, and `allow` authority modes;
5. the exact preview, approval, revalidation, effect and audit sequence;
6. Linux/Parrot/WSL and native Windows Edge boundaries;
7. the smallest local read-only evaluation path;
8. current public-alpha capabilities, limitations and feedback route.

Historical Pixelgrama and CubePath evidence remains under `docs/showcase` and dated
baselines. It is not fetched or served by the current public site.

## Assets and requests

The handler embeds:

- `assets/index.html`;
- `assets/app.css`;
- `assets/app.js`;
- `assets/social-card.svg`;
- `assets/social-card.png`;
- `assets/favicon.svg`;
- `assets/robots.txt`;
- `assets/sitemap.xml`.

The document performs exactly one same-origin public request: `GET /version`. The
browser accepts only bounded version, tool-count and commit fields for presentation.
In the isolated deployment, the server obtains that identity from one exact HTTPS
`/version` URL, rejects redirects and unexpected fields, and returns no upstream error
detail. Unavailable or malformed identity produces a generic unavailable state. No
browser request is made to MCP, console, GitHub, analytics or another origin.

The social cards are self-contained typographic SVG and PNG assets with no script,
external reference or runtime identity. The PNG is the canonical Open Graph and Twitter
preview. The page also publishes an SVG favicon, a same-domain sitemap and an explicit
robots policy. Canonical and social metadata use the HTTPS apex domain.

## Interaction and accessibility

- semantic header, navigation, main sections and footer;
- skip link and visible `:focus-visible` treatment;
- bilingual English/Spanish copy changed with `textContent` only;
- three authority tabs that support click and arrow/Home/End keyboard navigation;
- tab labels and descriptions ensure the comparison does not depend only on color;
- copy control with an explicit manual-copy fallback;
- live runtime status announced through `aria-live`;
- layouts for 320-pixel mobile width and wide desktop displays;
- reduced-motion behavior through `prefers-reduced-motion`;
- no horizontal body overflow, modal, autoplay or forced boot sequence.

The page states that reduced authority is not absolute safety. Operators still own
configuration, credentials, dependency posture and recovery.

## Public security boundary

The document CSP permits only same-origin CSS, JavaScript, images and `/version`. It
rejects inline script/style attributes, objects, forms, framing, base URL mutation and
third-party resources. Responses also set `nosniff`, frame denial, no-referrer,
restrictive permissions, same-origin isolation headers and `no-store` caching.
The isolated site also emits HTTP Strict Transport Security for the apex domain and its
subdomains. TLS termination remains owned by the managed deployment platform.

Tests must fail when the site:

- loses exact unauthenticated `GET /` or hardened 404/405 handling;
- changes `/mcp`, `/console` or `/version` route ownership;
- introduces inline executable content, remote assets, browser storage, cookies,
  WebSockets, SSE or control-plane requests;
- embeds moving release, commit, tool-count or catalog identity;
- restores historical showcase dependencies to the runtime landing;
- loses bilingual, mobile, keyboard or reduced-motion behavior;
- uses unsupported capability claims or generic marketing slogans.

Production closure requires the final pull-request HEAD to pass every applicable gate,
a merge commit into `main`, an isolated deployment built from that exact merge,
verified HTTPS for the selected domain, and a successful sanitized `/version` probe of
the configured control plane. The isolated deployment reads Coolify's predefined
`SOURCE_COMMIT` so `/healthz` exposes exact build identity without embedding a moving
value in the page. `AEONTRA_SITE_COMMIT` remains an explicit validated override for
other deployment platforms.
