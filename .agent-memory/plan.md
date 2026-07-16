# P11.2 execution plan

1. [done] Reconstruct Git and preserve Step 7 exactly.
2. [done] Audit and replace the incomplete Bubblewrap implementation with a complete fail-closed sandbox spec.
3. [done] Add adversarial unit coverage, exact provider/command validation, closed ToolPath policy and real tagged isolation smoke/report.
4. [done locally] Extend the no-network distributed Docker E2E with Debian Bubblewrap, pinned native OpenCode 1.18.1, four role PIDs, isolation report and release-report assertions.
5. [active] Remove temporary scratch, commit the single Step 8 candidate, push it and use GitHub Actions as the authoritative real Bubblewrap/Docker/race validation environment. Amend and force-push any correction so the final branch has no diagnostic history.
6. [pending] After Step 8 CI is green, create `docs/install-opencode-edge-parrot.md` and the dated P11.2 baseline with verified A/B/C benchmark values from the actual reports.
7. [pending] Run the complete final local/remote gate matrix on one exact tree, record `git write-tree`, update verified Brain conclusions, publish only `p11-2-remote-opencode-relay`, open PR against `main`, and stop when the final SHA is fully green.

Hard boundaries: no merge, deployment, pairing, real Parrot installation, Coolify changes, historical CodeQL closure, durable console sessions/telemetry, Goal Runtime, HTB/THM/VPN or general rootless build workcell.
