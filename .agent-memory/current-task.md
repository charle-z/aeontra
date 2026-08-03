# Current task — Hito 5 consequential GitHub broker operations

## Verified production base

- PR #132 merged at `842e4e27a9029627edbb0129f7ccd95d718d3360` after 16/16
  exact-head checks and made the managed Front Door preview expose its complete
  dual-catalog transition contract.
- PR #131 merged at `08070734b9827c8efda8d67e922b057f70f7b3d0` after 16/16
  exact-head checks and is deployed.
- Production serves protocol `2024-11-05`, 135 tools and catalog
  `sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.
- Managed Front Door deployment `x5a11ixcdfeo8c3e46ofry38` accepted the previous
  catalog only as one temporary authenticated transition catalog. After the backend
  replacement, deployment `thvv358cgxkzu4qv87z8hfp8` removed it. The final preview
  reports no transition catalog and no pending catalog change.
- Public `/mcp` returns the expected OAuth challenge with the new catalog, backend
  commit and 135-tool identity. Protected-resource and authorization-server discovery
  return valid JSON through Front Door commit
  `489a64f40cbbde014986ff130662a485f9513d6c`.
- Brain note `gpt-web-direct-edge-h5-gh-broker-preflight` retains the deployment
  evidence and authority boundary.

## Deployed Hito 5 slice

- Debian Edge packages depend on the official `gh` CLI.
- A human keeps a complete normal `gh auth login`; `mcp-edge github import-gh` copies
  that authority into separate owner-only Edge state without exposing it.
- The private direct broker executes only constructed, bounded `gh api` reads for the
  repository already bound to the selected development project.
- `project_github_status` returns safe repository metadata and closed contents, PR and
  Actions capability probes without accepting token, URL, endpoint, path or raw CLI.

## Next exact action

Continue Hito 5 as separate reviewable closed operations for consequential pull-request,
workflow-dispatch and signed-release effects. Real-device acceptance for Hitos 3A, 3B,
4 and this Hito 5 slice still requires an explicitly named immutable Edge release after
installed `p15.0.12`; do not infer that release number.
