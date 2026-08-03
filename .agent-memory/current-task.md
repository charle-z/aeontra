# Current task — signed bundled GitHub CLI transition

## Verified production base

- PR #132 merged at `842e4e27a9029627edbb0129f7ccd95d718d3360` after 16/16
  exact-head checks and made the managed Front Door preview expose its complete
  dual-catalog transition contract.
- PR #131 merged at `08070734b9827c8efda8d67e922b057f70f7b3d0` after 16/16
  exact-head checks and is deployed. PR #133 synchronized closure documentation at
  `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603`; production serves that commit with the
  same public catalog.
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

## Verified real-device state

- Official release `p15.0.13` was published from `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603`
  and installed once on the paired Parrot Edge. Bundle, service, pairing and process
  coherence were verified healthy.
- A human completed normal `gh auth login` as `charle-z`, and
  `mcp-edge github import-gh --owner charle-z` returned only
  `{configured:true,owner:"charle-z"}`. The credential remains in owner-only Edge
  state and was not exposed.
- The private direct broker executes only constructed, bounded `gh api` reads for the
  repository already bound to the selected development project.
- `project_github_status` returns safe repository metadata and closed contents, PR and
  Actions capability probes without accepting token, URL, endpoint, path or raw CLI.

## Confirmed updater gap and safe transition

`p15.0.13` still obtains `gh` only through the Debian dependency path. The signed
archive updater does not invoke APT, so a clean archive update cannot guarantee the
broker executable. The correction must not put an unsigned extra file beside a v2
manifest and cannot jump directly to manifest v3 because the installed v2 updater
does not understand it.

The active bridge change therefore keeps emitted bundles at manifest v2 while adding
v3 verification, the future `github-cli` component contract, fixed safe executable
resolution, and rollback-safe reconciliation of the future managed compatibility
link. After its exact-head gates, merge and one signed bridge release, the real Edge
can safely consume a second signed v3 release that embeds pinned official `gh`.

## Next exact action

Finish the v2-to-v3 bridge, publish and install its immutable signed release, then land
the separate v3 packaging change and install that release. Re-run the real
`project_github_status` preflight, then continue Hito 5 consequential operations and
real-device acceptance for Hitos 3A, 3B and 4. Hito 9 multiagent/task-graph work is
explicitly outside the current authorization and must not be started.
