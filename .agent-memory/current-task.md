# Current task

Historical deployed baseline remains unchanged: P16 has not modified `main`, Coolify, production or real Parrot.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`.

## Step 7 rootless BuildKit candidate

Step 6 head `37c02f0067d7aa0023bd87f204a37a6ab91f1ec8` passed 15/15 exact-head checks. Step 7 harness/package head is now `e3a8f39` (`Step 7: Add pinned rootless builder fixture`) on top of prior Step 7 commits through `66df274`.

Implemented privately with no public MCP tool or production install:

- `internal/buildspike`: closed rootless BuildKit config/build plan, 50/65/80 CPU candidates, memory/PID/I/O bounds, rootful/symlink endpoint rejection, delegated cgroup-subtree evidence, whole-process-group cancellation, bounded environment/output/artifact/cgroup metrics, and reusable bounded cache;
- `packaging/builder`: rootless systemd candidate, generated BuildKit config, offline rollback-capable installer, conservative remover, and official v0.31.2 staging script;
- the official Linux amd64 release archive, SBOM and Sigstore bundle are pinned by SHA-256; staging extracts only `buildkitd`, `buildctl` and `buildkit-runc` and publishes private root-owned state atomically;
- `.github/workflows/p16-builder-spike.yml`: Ubuntu 24.04 fixture that installs rootless prerequisites, stages the pinned release, starts systemd, verifies worker/cgroup ownership, builds the same commit twice with cache reuse, stops the full cgroup and exercises conservative removal;
- documentation and guard tests distinguish disposable CI evidence from real VPS calibration.

Local validation green before commit:

- `go test ./... -count=1`;
- `go vet ./...`;
- Staticcheck v0.7.0;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`;
- `internal/buildspike` coverage remains above its blocking 75% threshold.

Next:

1. Commit this current-task/handoff update and publish the branch once.
2. Hold the exact published SHA until all checks are terminal, including the new `Rootless BuildKit candidate fixture`.
3. Use Actions diagnostics/log chunks for any failure; do not guess or weaken rootless/cgroup controls.
4. If the disposable fixture passes, prepare the real VPS 50/65/80 calibration bundle and exact operator action. Do not begin Step 8 until Step 7 selects an engine/quota from dated VPS evidence or records a structural stop.
