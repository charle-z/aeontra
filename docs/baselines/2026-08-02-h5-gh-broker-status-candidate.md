# Hito 5 official GitHub CLI broker read-only slice — 2026-08-02

## Scope

This slice introduces the first direct Edge GitHub API operation without starting
OpenCode, an external model runtime or autopilot. It deliberately covers read-only
authority verification only; PR writes, workflow dispatch and signed release
publication remain later closed slices.

## Source identity

- Merge: `08070734b9827c8efda8d67e922b057f70f7b3d0`.
- PR: `#131`, merged after 16/16 exact-head checks.
- Public catalog: 135 tools.
- Catalog hash:
  `sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.

## Authority and secret boundary

- The Debian Edge package depends on the official GitHub CLI package `gh`.
- A normal, full `gh auth login` remains available to the human operator.
- `mcp-edge github import-gh` copies that login into the existing owner-only `0600`
  Edge store without changing the normal GitHub CLI profile.
- Direct operations run only constructed `gh api` argv outside the workcell.
- `GH_TOKEN` exists only in the bounded child environment under a private HOME/XDG
  root; token, URL, headers, paths and raw CLI output never enter MCP results.
- Repository identity comes from the registered project and must match the configured
  credential owner.

## Verification boundary

Focused credential, broker, operation, MCP schema/result and Debian package-contract
tests passed. The exact integration head passed the complete 16-check CI matrix before
merge. Production serves the merge with protocol `2024-11-05`, 135 tools and the exact
catalog above. Managed Front Door deployments first allowed the previous catalog as a
single temporary transition identity and then removed it after public MCP and OAuth
continuity checks. Live account login, signed bundle installation and real
private-repository verification remain device acceptance, not source claims.
