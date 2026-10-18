# Hito 4 persistent toolbox core candidate — 2026-08-02

This is candidate source evidence, not a claim about an installed Edge.

- Base: `origin/main` at `e7a2e6048c5a7a3a7fec77ee699653babaab244c`.
- Public additions: `project_toolbox_create`, `project_toolbox_status`,
  `project_toolbox_exec`, `project_toolbox_install`, and
  `project_toolbox_cleanup`.
- Candidate catalog: 130 tools at
  `sha256:11697746d4c61b4035c8a6413b4ad63a0b29a50d343cf64d524640fdb719d03d`.

The Edge owns a private opaque toolbox identity and `0600` metadata per registered
development workspace. The server fixes a Debian base, records the pulled image ID,
uses only the validated user-owned rootless container endpoint, mounts the selected
workspace at `/workspace`, and preserves the writable rootfs until explicit cleanup.
Callers cannot supply an image, host path, socket, volume, container identifier or
privileged flag.

Execution and installation accept arbitrary explicit argv, optional relative cwd and a
non-secret environment overlay. No implicit shell or language/package-manager
allowlist is added. Output is bounded and redacted. Public results exclude workspace
paths, sockets, engine/container identifiers, argv and environment.

Automated candidate evidence covers persistence across manager reopen, owner binding,
unsafe/symlinked metadata, missing rootless authority, arbitrary argv construction,
explicit cleanup, operation-kind validation and MCP schema/output filtering. Service
lifecycle, repair, background integration and real Edge restart acceptance remain the
next Hito 4 slice.
