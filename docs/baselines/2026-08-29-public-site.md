# Public product site acceptance — 2026-08-29

Status: **dated operational evidence**

This file records the first accepted production deployment of Aeontra's isolated
public product site. It is not a moving source of repository or control-plane identity.

## Source and delivery

- Pull request `#249` passed all 18 exact-head checks and merged as
  `5c1ee5b59f45b40cfa77a891271ada7fd1573855`.
- Production verification of that merge found that a literal environment reference did
  not interpolate the platform-provided source commit.
- Pull request `#250` added the validated `SOURCE_COMMIT` fallback, passed all 18
  exact-head checks and merged as
  `e084ac02a2440b8fc055a8188020fcd008f0301c`.
- Managed deployment `bs6zifxam5wwejzrfeixs44o` finished from that exact merge. The
  application reported `running:healthy` and its bounded log contained only the site
  listener startup message.

## Public HTTP acceptance

`https://aeontra.com/` and its canonical assets returned successful HTTPS responses with
`Strict-Transport-Security: max-age=31536000; includeSubDomains`:

- `/` returned the isolated HTML product site;
- `/healthz` returned site version `0.2.0` and exact source commit
  `e084ac02a2440b8fc055a8188020fcd008f0301c`;
- `/version` returned the sanitized identity of the separately deployed control plane;
- `/robots.txt`, `/sitemap.xml`, `/favicon.svg`, the canonical PNG social card and the
  embedded CSS and JavaScript assets returned their expected content types;
- `https://www.aeontra.com/` redirected to the HTTPS apex domain.

The site build identity and proxied control-plane identity are intentionally separate.
`/healthz` identifies the isolated site artifact; `/version` describes the live MCP
control plane and may advance on an independent deployment schedule.

## Browser acceptance

The production page was exercised in an interactive browser at 1366 by 768 and 390 by
844 pixels. Both viewports had no horizontal overflow and kept the primary actions in
the initial viewport. The English/Spanish control changed the visible copy, the
authority tabs supported click and End-key navigation, canonical/Open Graph/Twitter
metadata resolved to the HTTPS apex domain, and the browser reported no page errors.

## Boundaries

- No MCP, OAuth or console route was added to the isolated site process.
- No analytics, remote font, third-party image or tracking request was introduced.
- This acceptance did not redeploy the control plane or update an Edge bundle because
  the landing change did not alter the MCP catalog or Edge runtime.
- Search-engine indexing remains externally scheduled and is not claimed by this
  acceptance.
