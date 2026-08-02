# Handoff — official connector catalog-transition recovery

The active incident is deterministic catalog incompatibility, not backend failure or
CPU saturation. Backend `ecc1bef21aab0a144a9129a9d9907313661658c5` is healthy with
115 tools and catalog
`sha256:d1dab9c0d265284dc66d8c07a0c78b59aa1bd5d89d256255ab5862268e858bfb`.
Front Door `o338wpoy1254d83ud2y8p1v8` runs
`ced84aade8a691e487b4ca7448a87df42c9da0cb`, is process-healthy, but still pins only
the former 114-tool catalog. Its `/front-door/version` reports
`backend_incompatible`; official MCP and OAuth routes therefore return 503.

Branch `codex/front-door-catalog-transition` implements:

- one exact required primary catalog and at most one exact distinct transition hash;
- startup rejection for empty primary, malformed, duplicate or third catalogs;
- the same allowlist on probes, proxied MCP responses and SSE;
- OAuth discovery, authorization, token and DCR routing independent from MCP admission;
- managed transition planning from authenticated existing Coolify environment metadata;
- plan revalidation, normal deployment on catalog-state change and exact deletion of
  the transition environment entry during retirement.

No MCP tool schema or public description changed, so the backend catalog remains exactly
115 tools with hash `d1dab9c0d265284dc66d8c07a0c78b59aa1bd5d89d256255ab5862268e858bfb`.

Local evidence:

- affected package and documentation suites: green;
- full suite: all affected packages green; two unrelated 10-second provider fixtures
  timed out under aggregate load and passed isolated;
- Windows/DrvFS cannot reproduce the Linux `0755` packaging mode assertion;
- WSL `go vet ./...`, `go build ./...` and `git diff --check`: green.

Next: commit, publish, PR to main, exact-head CI, merge commit, then a separate reviewed
advance of `front-door-stable`. Deploy only the existing Front Door once with the former
and new hashes. Validate real public OAuth/DCR and MCP continuity. Retire the former hash
in a later managed reconciliation and prove rejection before release/Edge work.
