# Latest handoff — P12 rootless lifecycle correction

Date: 2026-07-18
Branch: p12-trusted-linux-workcell.
Pull request: #25.
Published diagnostic HEAD: 15adccc016dd690a54336ca2d54423eff4ee74d9.
Base: origin/main at 087f00e404855cc83e76c1eb7d6ed85ab14577c5.

Historical foundations remain explicit:
- P8.1 is closed, deployed and tagged p8.1 at d343264bffdc0ae1bc045a9d723e913be977090c.
- Its historical catalog had 67 tools and Edge state not_paired.
- P9 Brain is deployed at 4fbe1dda02351c632e67c0f10a5c5b314df745e2.
- P11.2 Remote OpenCode Relay remains the sandbox and relay foundation.
- The catalog remains exactly 85 tools with sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941.

Commit 15adccc added bounded failure-only rootless diagnostics and completed all 15 PR checks green with evidence complete. It is intentionally not the merge candidate because it still ran one rootless cycle.

The current working correction fixes the concrete lifecycle race: execContainerCommandRunner created a process group but context cancellation killed only the leader, so a child could survive. A deterministic heartbeat regression now proves the whole group stops. Cleanup now removes Podman pods before containers, then networks and volumes. The E2E derives two distinct runtime IDs per GitHub run, executes two clean cycles in one job, validates Compose readiness/down, PostgreSQL healthcheck/query, Chromium, the sole workspace project bind, process-group cancellation, rootful socket inaccessibility, service restart and no inherited resources. An EXIT trap cleans both labels, stops the user-owned Podman process group, removes the temporary socket and restores rootful socket permissions.

Final local gates pass: focused suites, the process-group regression repeated ten times, tagged compilation, full serial tests, atomic coverage thresholds, vet, build, Staticcheck, Govulncheck, Actionlint and diff validation. Only the correction commit, publication and exact-head CI remain pending. No real Parrot host was installed, paired or modified.

Correction `39e8475` was published. Both rootless runs then failed with the redacted category `workspace_bind`; the cause was a shell-probe false negative, not a Podman bind failure. BusyBox `sh` could terminate when the failed redirection of the `printf` builtin was evaluated. The pending fix uses external `touch` under an `if` and separately inspects Podman mounts to prove the workspace is the sole project bind. Probe fix `d37c99146e053a9bada19822d4fcb7effbf3f7cf` is published and completed 15/15 exact-head checks green with mergeable=true and evidence complete. Both rootless jobs passed the two-cycle lifecycle, restart, cleanup trap and evidence verification. This memory synchronization is the only remaining branch change; after its checks are green, revalidate PR #25 and merge by merge commit.
