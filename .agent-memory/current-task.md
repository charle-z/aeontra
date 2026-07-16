# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published SHA: `54785285c6fe6659d9bec15cf22486905e061cf1`
Published tree: `491c6f8fd652c3bdb0c666e4b4e08c23b51b0aad`
Upstream: `origin/p11-2-remote-opencode-relay`
Base: `01fde5067752ab1c43424d2d54f9afd914617ba5`
PR: `https://github.com/charle-z/mcp-devbox/pull/13` (draft)

## Historical deployed state

P9 Brain was deployed before its successor. P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. This historical production state remains unchanged by P11.2.

## Reconstructed state

Git matches the confirmed state. CI and Security Evidence for SHA `54785285c6fe6659d9bec15cf22486905e061cf1` are green. E2E run `29537366806` failed only because `TestRemoteOpenCodeDistributedRelay` reported `remote_repository_not_modified`; direct normal and direct restart passed. Bubblewrap host isolation was skipped only because the host job depends on the failed distributed job.

## Diagnosed defect and validation

The defect was isolated to the Docker-only relay adapter. OpenCode's `--dir` was translated to the real fixture, while later model tool arguments still used the virtual `/workspace`. The test now supplies the actual fixture path only in `relay_container_e2e`; the real Bubblewrap host path remains `/workspace`.

The real OpenCode 1.18.1 remote integration passed ten consecutive runs after temporary allowlisted diagnostics proved that the edit changed the target digest and bash tested the same fixture. The final version contains no temporary diagnostics. It permanently asserts canonical completed tool-result states, a changed target digest, semantic source modification, green tests, completed runtime and zero duplicates.

## Immediate objective

Complete local gates, checkpoint memory, commit and publish the focused fix, then require a new exact-SHA distributed E2E followed by Bubblewrap host isolation and the combined sandbox E2E.

Do not modify Bubblewrap, capabilities or the host runner without a new demonstrated regression.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
