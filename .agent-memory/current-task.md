# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Local validation SHA: `d3d57668504e1d38edfee7baf9f18379e37f205f`
Local validation tree: `dca0dd337e8839fc84c3caad98d541b938270d01`
Upstream before publish: `origin/p11-2-remote-opencode-relay`
Base: `01fde5067752ab1c43424d2d54f9afd914617ba5`
PR: `https://github.com/charle-z/mcp-devbox/pull/13` (draft)

## Historical deployed state

P9 Brain was deployed before its successor. P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. This historical production state remains unchanged by P11.2.

## Remote repository modification fix

Commit `d3d57668504e1d38edfee7baf9f18379e37f205f` fixes the Docker-only relay workspace mismatch. OpenCode's `--dir` and its later read/grep/edit/bash arguments now refer to the same temporary fixture in `relay_container_e2e`; the real Bubblewrap host path remains `/workspace`.

The integration permanently asserts canonical completed tool-result states, an initial/final target digest change, semantic source modification, green fixture tests, completed runtime and the measured `repository_modified` value. Temporary diagnostics and helpers were removed.

The real OpenCode 1.18.1 remote integration passed ten consecutive runs. Local serial tests, docs, vet, build and diff check are green.

## Immediate objective

Publish this exact validation SHA without force, observe a new exact-SHA distributed E2E, and require direct normal/restart, remote normal/restart, repository modification, four completed tools, tests, runtime completion, request_ref and zero duplicates. Only then allow Bubblewrap host isolation and the combined sandbox E2E to run.

Do not modify Bubblewrap, capabilities or the host runner without a new demonstrated regression.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
