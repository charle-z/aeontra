# Current task — Hito 5 official GitHub CLI broker

## Verified production base

- Branch base: `origin/main` at `1b6b2073ab4090ba899cb2294104a679fbcb2d99`.
- PR #130 passed 16/16 exact-head checks, merged and auto-deployed.
- Backend serves 134 tools at
  `sha256:9d8bea913bb9c0da9467dc0cfff414e02acd3893f1246f7a7e8e3d6a5a859236`.
- Front Door retirement deployment `ur9orp1o903bwfbbtl6xxp7f` finished with only the
  current catalog allowed. Public MCP challenge and OAuth discovery are healthy.
- Brain note `gpt-web-direct-edge-h4-rootless-controls` records the Hito 4 source
  closure; real-device acceptance still awaits an explicitly named signed release.

## Active source candidate

- Branch: `codex/persistent-github-broker`.
- Debian Edge packages now depend on the official `gh` CLI.
- A human may retain a complete normal `gh auth login`; `mcp-edge github import-gh`
  copies that authority into the separate owner-only Edge store without exposing it.
- The private direct broker executes only fixed `gh api` reads with `GH_TOKEN` in the
  bounded child environment and a private HOME/XDG root.
- `project_github_status` accepts only registered project alias and Edge target and
  returns safe repository metadata plus contents, PR and Actions capability probes.
- Candidate catalog: 135 tools at
  `sha256:557d3cbb956a311429dbcf893b329b5b0d0dea5e38a0c8dbae96abac52b1e7dd`.

## Next exact action

Complete full gates and exact documentation consistency, commit the Hito 5 read-only
slice, publish a PR, perform the managed dual-catalog Front Door transition, merge and
deploy, then retire the previous catalog. Continue Hito 5 consequential PR, workflow
and release operations in later reviewable slices. Do not infer a signed Edge release
number after installed `p15.0.12`.
