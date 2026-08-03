# Handoff — p15.0.15 rolled back; atomic Edge handoff corrective active

Production backend commit `f8c0ce6a25ed46ae4bc4031656b04eb4c3e88603` serves
protocol `2024-11-05`, 135 tools and catalog
`sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.
PR #131 and its integration head passed 16/16 exact-head checks before merge.

The official `gh` package dependency, normal full-account `gh auth login`, private
`mcp-edge github import-gh`, fixed bounded `gh api` broker and read-only
`project_github_status` operation are in source and backend production. Credentials
remain outside the model workcell and only bounded parsed metadata reaches MCP.

Front Door commit `489a64f40cbbde014986ff130662a485f9513d6c` stayed healthy
through the managed two-deployment transition. Deployment
`x5a11ixcdfeo8c3e46ofry38` temporarily accepted both authenticated catalogs while the
backend changed; deployment `thvv358cgxkzu4qv87z8hfp8` then retired the previous
134-tool catalog. A final managed preview reports an empty transition catalog and no
pending catalog mutation. Public MCP challenge and OAuth discovery return the new
identity.

Official release `p15.0.13` was published from the production commit and installed
once on the paired Parrot Edge. Bundle and service are healthy. The human login as
`charle-z` and `mcp-edge github import-gh --owner charle-z` completed without exposing
the credential.

PR #134 merged the v2 bridge at `e78436da697db634be4159ce86a7116871bb7c4f`
after 16/16 checks, and official run `30776878699` published `p15.0.14`. Its files and
current link installed successfully. A previously enabled legacy
`mcp-devbox-edge.service` kept the old `p15.0.13` process and state lock, so the
templated managed unit failed closed instead of creating a duplicate.

PR #135 merged at `6b6c3bcf7c019ebad4568baca8114ad7407d0565` after
16/16 checks and official run `30778625101` published signed `p15.0.15` with pinned
official `gh` 2.97.0. Real operation `eo_a5b534df08797bd152d018d5fc1a65be`
failed closed and restored `p15.0.14`. The updater attempted `disable --now` against
the legacy service whose Edge process had launched and was awaiting that updater, so
the managed unit never completed the handoff and the old process retained the lock.

The active corrective disables persistence without stopping the caller and gives the
managed unit fixed `Conflicts` plus `After` ordering for the two legacy unit names.
Finish its gates, merge, next signed release and one real update; then require one
managed current process and run the live GitHub preflight. Hito 9
multiagent/task-graph work remains deferred explicitly.
