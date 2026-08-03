# Handoff — Hito 5 official GitHub CLI broker candidate

Production is `1b6b2073ab4090ba899cb2294104a679fbcb2d99`, 134 tools and catalog
`sha256:9d8bea913bb9c0da9467dc0cfff414e02acd3893f1246f7a7e8e3d6a5a859236`.
The managed Front Door has retired all older catalogs and OAuth/MCP are healthy.

Branch `codex/persistent-github-broker` starts Hito 5 without OpenCode or another model
runtime. It adds the official `gh` package dependency, lets a human keep a full normal
GitHub CLI login, and adds `mcp-edge github import-gh` to copy its token into separate
private Edge state. The import returns only configured/owner and does not alter the
normal CLI profile.

The direct Edge broker uses only constructed `gh api` argv for the repository already
bound to the selected development project. The token exists only in the child process
environment under a private runtime root. `project_github_status` exposes bounded
repository identity and closed metadata/contents/PR/Actions capability booleans; it
does not accept or return token, URL, endpoint, headers, path or arbitrary CLI text.

Candidate identity is 135 tools at
`sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.
Focused TDD tests are green. Run complete gates, publish/merge with the managed
dual-catalog transition, then continue Hito 5 writes as separate closed operations.
Do not infer the next immutable signed release after installed `p15.0.12`.
