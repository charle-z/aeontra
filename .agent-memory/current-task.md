# P12 — Trusted Linux Workcell

Branch: `p12-trusted-linux-workcell`.
Pull request: `#25` against `main`.
Published diagnostic HEAD: `15adccc016dd690a54336ca2d54423eff4ee74d9`.
Base remains `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5` until merge.

Historical deployed foundations preserved:
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P11.2 Remote OpenCode Relay remains the sandbox/relay foundation.
- Public catalog remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed P12 sequence includes Steps 1–6 and the CI fixture/path/Chromium/ShellCheck corrections through `549271b`. Commit `15adccc` added failure-only redacted rootless diagnostics; its exact PR head completed all 15 checks green with evidence complete, but it is not the merge candidate because it still exercised only one rootless cycle.

Current uncommitted lifecycle correction:
- fixes the concrete cancellation race in `execContainerCommandRunner`: the command created a process group but context cancellation killed only its leader, allowing child processes to survive;
- adds a deterministic regression proving a child heartbeat stops after cancellation;
- cleans Podman pods before containers, then networks and volumes, all by exact runtime label;
- removes the fixed rootless E2E runtime identity and derives two distinct IDs from the GitHub run;
- executes two consecutive rootless cycles in one job, with the second proving the first left no resources;
- validates a user-owned rootless socket, rootful socket inaccessible to the test user, one workspace project bind, image build, explicit pod, Compose readiness/down, PostgreSQL healthcheck/query, Chromium, cancellation, cleanup and service restart;
- wraps the complete service lifecycle in an EXIT trap that cleans both runtime labels, stops the Podman process group, removes the temporary socket and restores rootful socket permissions;
- keeps failure diagnostics bounded and allowlisted, preserving the original exit status and uploading no log on green runs.

Final local gates pass on the correction tree: focused rootless/redactor/workflow/docs suites, the process-group regression repeated ten times, tagged `p12_e2e` compilation, full serial tests, atomic coverage with every threshold green, vet, build, Staticcheck v0.7.0, Govulncheck v1.6.0 with no vulnerabilities, Actionlint v1.7.12 and `git diff --check`.

No real Parrot installation, pairing, VPN action or host modification has been performed.

Next exact action: complete full local gates, remove every temporary/generated file, commit the lifecycle correction, publish it, require all exact-head checks green simultaneously, then merge PR #25 by merge commit and verify the automatic deployment.
