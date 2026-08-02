# Handoff — Hito 4 resource controls candidate

PR #129 merged as `f902fc4a229503a05eb47fa9ac4b3137b55d46f2`, production
serves 134 tools at
`sha256:504e6f371de9a46a6e255913a019a9990d8977de286fa4f51d90f27fdf06308b`,
and stable Front Door deployment `vll165rrqplfulnw8oyh1ucs` retired the previous
catalog. Public OAuth discovery and the unauthenticated MCP challenge are healthy.

Branch `codex/toolbox-container-controls` adds optional CPU millicores, memory MiB and
process-count limits to `project_toolbox_create`. Defaults are 4000/8192/2048 and
ranges are closed. Private owner-only metadata binds them to the persistent container;
every operation checks live memory bytes, nano-CPU quota and PID cap. A caller cannot
silently resize an existing toolbox, and drift fails as an ownership error.

Focused tests pass and a real temporary rootless Podman container returned exactly
8589934592 memory bytes, 4000000000 NanoCpus and PidsLimit 2048 before cleanup. The
resource-limit-only commit kept 134 tools at
`sha256:14de29c2c2c7dca8ba6d0621f57495940f88975d4fea0bd97a72f91848b03b84`.
The combined rootless-engine candidate is 134 tools at
`sha256:9d8bea913bb9c0da9467dc0cfff414e02acd3893f1246f7a7e8e3d6a5a859236`.

PR #130 at head `c4645faf6b5a68561e1fe889cbe600d056a65757` contains the two
committed slices. A real toolbox-shaped container installed Podman, reported its remote
engine as rootless and launched an auto-removed Alpine child returning `nested-ok`;
all temporary resources were removed.

The first CI run exposed an existing ordering race in project-process reconciliation:
the kill receipt could win over the intended `process_logs_incomplete` failure. The
minimal candidate correction persists the terminal failure before killing the unsafe
process. The exact failing test is green 20/20 under `-race`; the full race run passed
`internal/edgeclient`, while an independent policy performance assertion later exceeded
its wall-clock threshold under host load and passed three isolated repetitions.

Commit and publish that correction, obtain exact-head green gates, perform the managed
dual-catalog Front Door transition, merge and deploy, then retire the previous catalog.
Hito 4 still needs real-device release proof. The installed Edge remains `p15.0.12`;
do not infer the next immutable release number.
