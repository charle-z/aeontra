# Current task — P12 Parrot onboarding hardening and Cubethon readiness

Branch: `p12-parrot-onboarding-hardening`.
Base: `origin/main` at `3946fd7033f28906deb932298387034e2fa27fe8`.

Historical deployed foundations preserved:
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P11.2 Remote OpenCode Relay remains the sandbox/relay foundation.
- P12 Trusted Linux Workcell was merged by PR #25 at `3946fd7033f28906deb932298387034e2fa27fe8`.
- Public catalog remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Real Parrot WSL validation completed on 2026-07-18 with runtime `mr_829f6601fca6f887bc2d0133a4c5dff1`, workspace `ws_7c4686f5d9244bbad30ae705d4b660c5`, state `completed`, six sequences, exact `edge-smoke.txt`, and clean `git diff --check`.

This follow-up fixes the production gaps exposed by onboarding:
- Bubblewrap under systemd requires `AF_NETLINK` for `NETLINK_ROUTE` loopback setup.
- Bubblewrap verification discarded bounded stderr and returned only a generic failure.
- early local journal failures could leave the remote runtime stuck in `starting`.
- the journal permanently rejected a legitimate rerun of the same completed objective.
- `/mnt/wsl/resolv.conf` is a WSL system file, not a forbidden Windows workspace mount.
- the Parrot/provider/systemd installation docs were stale and contained non-runnable commands.
- onboarding needed one packaged, executable preflight/smoke path.
- README/P12 status and Cubethon presentation needed to reflect merged/deployed/validated reality without overstating isolation.

Current implementation adds safe NETLINK diagnostics, bounded Bubblewrap stderr classification, remote failure propagation, transactional journal migration, repeatable terminal objectives, the packaged OpenCode Edge systemd template, a Parrot onboarding preflight, production evidence, and a Cubethon submission draft.

Do not merge or deploy until focused tests, full gates, review, exact-head CI, and the final public presentation checks are complete.
