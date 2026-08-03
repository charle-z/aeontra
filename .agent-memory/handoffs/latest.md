# Handoff — Hito 5 read-only GitHub broker deployed

Production backend commit `08070734b9827c8efda8d67e922b057f70f7b3d0` serves
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

Continue Hito 5 with separate closed preview/execute operations for PR writes,
workflow dispatch and official signed-release publication. Do not infer the next
immutable signed Edge release after installed `p15.0.12`; real-device import and broker
acceptance wait for an explicitly named release.
