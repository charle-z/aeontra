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

## Post-baseline lifecycle finding

Later real-device acceptance installed `p15.0.13`, completed the normal `gh` login and
import, and found that archive-only updates do not resolve this baseline's Debian
dependency. Historical evidence above is unchanged. The corrective design uses one
manifest-v2 bridge release followed by a manifest-v3 release that cryptographically
binds pinned official `gh`; private-repository status remains pending until that
transition is installed.

Signed `p15.0.14` later installed the v2 bridge. Its real activation exposed an older
enabled `mcp-devbox-edge.service` that retained the `p15.0.13` process and state lock;
the managed templated unit failed closed rather than duplicating the Edge. PR #135 and
signed `p15.0.15` added exact retirement plus the bundled CLI, but the first real
activation rolled back because stopping the legacy caller inside its own updater
prevented a complete handoff. PR #136 and signed `p15.0.16` changed that to
disable-only persistence retirement plus fixed systemd conflict ordering. The operator
completed the one-host handoff manually, including disabling the identity watcher,
but `p15.0.16` still failed activation and restored `p15.0.14`. The exact unit delta
was only that conflict/order pair, so the post-handoff release removes it and leaves
an active unpackaged legacy service as an explicit fail-closed operator migration.
This note records the later findings without changing the baseline's original claims.

PR #137 later removed the obsolete conflict pair and official run `30782426563`
published signed `p15.0.17`. Its one real activation still restored `p15.0.14`, now
before new-unit installation. The v2 root updater sandbox omitted `/usr/local/bin` from
its strict write paths because its own bundle did not carry `gh`; manifest-v3 was the
first activation to require the fixed managed link. The package correction adds only
that path to the closed update and rollback units and requires one signed package
upgrade so the already-running root sandbox is actually replaced.
