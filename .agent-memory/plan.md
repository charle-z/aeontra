# Plan — P12 Linux Workcell MVP

1. Extend the local workspace registry with a backwards-compatible schema for `linux-workcell`, default `dev`, and optional typed HTB metadata. Add opt-in CLI registration/configuration/inventory commands without exposing host paths to the VPS.
2. Add local preflight and rendered instruction/state files. Enforce allowed Linux roots, HTB interface/route/LHOST checks, idempotent directory structure, and resume hints.
3. Split OpenCode sandbox construction into unchanged `sandbox` and opt-in `linux-workcell` policies. Keep filesystem isolation, use host-shared network honestly, expose Parrot/system tools read-only, add workspace-private tool/cache/runtime prefixes, and support rootless Docker/Podman without rootful sockets.
4. Add process-group cancellation, runtime-labelled rootless resource cleanup, tool inventory, and HTB template rendering.
5. Add unit/integration fixtures for dev, HTB, sandbox regression, cancellation, cleanup, memory, and catalog invariants.
6. Synchronize docs/baseline/handoff; run local gates; commit reviewable steps.
7. Publish the branch, open a PR, wait exact-SHA checks, correct failures, merge by merge commit, observe automatic deployment, and report Parrot setup steps. Do not modify the real Parrot machine.
