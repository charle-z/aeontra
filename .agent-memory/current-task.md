# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published Step 8 commit under correction: `b2f8d3b11c2bcbc08433f8ac9679aa1fe61ed6f5`
Published Step 8 tree under correction: `e4b8da331f31d55f1463615fa82bcf4c5184fe2e`
Step 7 commit: `ef2cb7eee4ecb67e5526fc1d055a482edd92877e`
Step 7 tree: `f13fbcdb28ead77a4a17530a452f88249f9d6135`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`
Draft PR: `https://github.com/charle-z/mcp-devbox/pull/13`

Historical deployed baseline retained for release synchronization: P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`. P9 Brain is deployed. P11.2 does not modify those releases.

Authoritative CI finding for first Step 8 SHA:
- E2E run `29527418921`, job `87719193348` reached the real `TestBubblewrapRealIsolationSmoke` and failed immediately with redacted category `permission`, before any report was produced.
- The container already used `seccomp=unconfined`, non-root user, `--cap-drop ALL`, read-only root, `--network none`, no Docker socket and no privileged mode.
- GitHub hosted runner is Ubuntu 24.04. The remaining host policy boundary is the Docker AppArmor profile restricting Bubblewrap's unprivileged user namespace.

Correction prepared:
- add only `--security-opt apparmor=unconfined` to the ephemeral E2E container;
- retain `seccomp=unconfined`, `no-new-privileges`, non-root execution, dropped capabilities, read-only root, no network, no host PID/IPC, no Docker socket and no privileged mode;
- do not change global host user-namespace settings or load a persistent host profile.

Local correction checks: `git diff --check` is green. Next action: stage the current checkpoint and run.sh correction, amend `Step 8: isolate OpenCode runtime with Bubblewrap`, force-update the branch with lease, then inspect the new PR workflow runs. Do not begin Step 9 until the real Bubblewrap E2E is green.
