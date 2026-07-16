# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Current published validation SHA: `d1bc48d79a3edc1ec43d5bbaa717ed87834d8848`
Current published validation tree: `bdd9791e6c77254988ca80853e7a107e6ebe5d6d`
Step 7 commit: `ef2cb7eee4ecb67e5526fc1d055a482edd92877e`
Step 7 tree: `f13fbcdb28ead77a4a17530a452f88249f9d6135`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`
Draft PR: `https://github.com/charle-z/mcp-devbox/pull/13`

Historical deployed baseline retained for release synchronization: P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. P9 Brain is deployed. P11.2 does not modify those releases.

Authoritative CI findings:
1. SHA `b2f8d3b11c2bcbc08433f8ac9679aa1fe61ed6f5`, E2E run `29527418921`: Bubblewrap failed immediately with permission category because Ubuntu 24.04 AppArmor blocked unprivileged user namespaces. Correction: ephemeral Docker container uses `apparmor=unconfined`; all other restrictions remain.
2. SHA `d1bc48d79a3edc1ec43d5bbaa717ed87834d8848`, E2E run `29527857924`, job `87720651987`: Bubblewrap advanced past user-namespace creation but exited with `process_exit` before writing the isolation report. The remaining likely boundary is mount propagation (`Failed to make / slave`) because the container drops every capability, including `CAP_SYS_ADMIN` from the bounding set.

Second correction prepared:
- grant only `SYS_ADMIN` to the ephemeral E2E container after `--cap-drop ALL`;
- retain non-root `edge`, `no-new-privileges`, AppArmor/seccomp override only for namespace syscalls, read-only root, network none, no Docker socket, no host PID/IPC, no devices and no privileged mode;
- add redacted `slice_code` classification for user-namespace, UID-map, mount-propagation, mount, exec and missing-path failures so future CI diagnostics do not expose paths or command data.

Local correction gates green:
- tagged `TestBubblewrapFailureCode`;
- `go test -p 1 ./... -count=1`;
- `git diff --check`.

The remote branch temporarily contains validation commits because force-push is blocked by the public MCP policy. Once Step 8 E2E is green, rebuild one canonical `Step 8: isolate OpenCode runtime with Bubblewrap` commit from the validated tree with parent `ef2cb7eee4ecb67e5526fc1d055a482edd92877e`, update the branch to it and remove all CI diagnostic history before Step 9.

Next action: delete `.agent-scratch/`, commit and publish the minimal SYS_ADMIN plus safe diagnostic correction, then inspect the new E2E report. Do not start Step 9 until Bubblewrap, OpenCode/provider and all four distributed scenarios pass.
