# Hito 5 official GitHub CLI broker status candidate — 2026-08-02

## Scope

This candidate introduces the first direct Edge GitHub API operation without starting
OpenCode, an external model runtime or autopilot. It deliberately covers read-only
authority verification only; PR writes, workflow dispatch and signed release
publication remain later closed slices.

## Candidate identity

- Base: `1b6b2073ab4090ba899cb2294104a679fbcb2d99`.
- Branch: `codex/persistent-github-broker`.
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
tests must be green before publication. Full Go tests, race, vet, build,
documentation consistency and exact-head CI remain release evidence. Live account
login, signed bundle installation and real private-repository verification remain
device acceptance, not source claims.
