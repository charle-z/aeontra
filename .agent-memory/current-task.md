# Current task

Date: 2026-07-16
Branch: `p11-2-remote-opencode-relay`
Published validation SHA: `06340da7d7f0c5e21d9f3218306c49f94b5b760f`
Published validation tree: `20638a3ac204a02a3f44536ad64478ee4194a377`
Upstream: `origin/p11-2-remote-opencode-relay`
Base `origin/main`: `01fde5067752ab1c43424d2d54f9afd914617ba5`
Draft PR: `https://github.com/charle-z/mcp-devbox/pull/13`

## Preserved deployed baselines

- P8.1 Console 2.0 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed; P11.2 does not alter either deployed release.

## Second remote validation result

E2E run `29536609450` failed only in the unprivileged Docker relay. The host Bubblewrap job was correctly skipped because it depends on relay evidence. The safe failure remained `opencode_provider`.

The exact root cause is now demonstrated: the test-only adapter translated provider JSON by sequential global replacement. It converted `/mcp-provider` to `/workspace/integrations/opencode/provider`, then translated the newly inserted `/workspace` prefix again to the temporary runtime workspace. OpenCode therefore received a nonexistent provider path. This was a harness-only path translation defect, not a Bubblewrap or product failure.

## Pending third validation tree

The working tree now:

- parses `OPENCODE_CONFIG_CONTENT` structurally;
- translates the provider npm path and driver socket path exactly once;
- preserves `webfetch` and `websearch` deny;
- keeps `external_directory` allow only in the test-tagged Docker adapter;
- adds a regression that detects the exact double-translation failure;
- adds `docs/install-opencode-edge-parrot.md` with Parrot WSL2, systemd, dedicated user, Bubblewrap preflight, pinned OpenCode/provider integrity, registry, pairing, service, heartbeat, modes, cancellation, kill switch, revocation, update, rollback and uninstall.

Production and host Bubblewrap code are unchanged by this correction.

Local gates green before the next commit:

- `go test -p 1 ./... -count=1`;
- `go test -tags=opencode_e2e -p 1 ./... -count=1`;
- regression repeated ten times;
- Actionlint v1.7.12;
- `git diff --check`.

Next action: commit and publish the structured translation correction plus Parrot guide, observe the new E2E run, and extract metrics only from green artifacts.

## Boundaries

No merge, deployment, pairing, real Parrot installation, tag, Coolify change, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL remediation.
