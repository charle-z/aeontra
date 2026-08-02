# Handoff — Hito 4 persistent toolbox core candidate

Safe Edge checkout sync merged through PR #127 at
`e7a2e6048c5a7a3a7fec77ee699653babaab244c`, deployed with 125 tools, and reconciled
through the stable Front Door. Deployment `g13z5dohfncg8r16cv7w4ngn` retired the old
catalog. OAuth discovery and the unauthenticated MCP challenge are healthy. Brain note
`edge-safe-checkout-sync-deployed` records the closure and signed-release constraint.

Current branch `codex/persistent-toolbox` is based exactly on that merge. It adds five
direct tools for a persistent rootless Debian toolbox: create, status, arbitrary exec,
install and explicit cleanup. The Edge fixes the base and private container identity,
records exact image identity in owner-only `0600` metadata, mounts only the selected
workspace at `/workspace`, and uses the already validated rootless Podman/Docker
endpoint. Callers cannot provide images, host paths, sockets, volumes, container names
or privileged flags.

Manager tests prove persistence across reopen, project/target ownership, arbitrary argv
construction, explicit cleanup, missing rootless authority and symlinked metadata
rejection. Edge operation and MCP tests bind each result to its operation kind and keep
paths, engine/container identity, argv and environment out of public output. Candidate
identity is 130 tools at
`sha256:11697746d4c61b4035c8a6413b4ad63a0b29a50d343cf64d524640fdb719d03d`.

The next safe action is full focused/docs validation, diff review, commit/PR/exact-head
CI/merge/deploy/catalog retirement. Then add service start/status/stop, repair and
background integration on the same toolbox identity. The immutable Edge release
version remains an external authorization constraint; do not infer one after
`p15.0.12`.
