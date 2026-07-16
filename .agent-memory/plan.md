# P11.2 execution plan

1. [done] Diagnose and correct the Docker-only remote workspace mismatch.
2. [done] Prove the real OpenCode 1.18.1 remote integration ten consecutive times and remove temporary diagnostics.
3. [done] Publish validation SHA `2bda26d4382feca7b9367a068dfbeff917e5538d`.
4. [done] Require exact-tree distributed relay, Bubblewrap host isolation, combined sandbox, CI and Security Evidence; all passed for tree `e8862ee9229ec8a98237251de6d3272e3f72ee1e`.
5. [done] Extract green report metrics and synchronize the dated P11.2 baseline, current 78-tool catalog evidence, documentation map, roadmap, capsule, README and AGENTS.
6. [done] Remove all scratch artifacts and run final local formatting, serial suite, coverage thresholds, vet, build, Staticcheck, Govulncheck, Actionlint, Node provider tests and provider/driver normal/restart/remote integrations.
7. [active] Run final documentation/diff checks, create canonical commit `Step 8: isolate OpenCode runtime with Bubblewrap`, and record its exact SHA/tree.
8. [pending] Publish the canonical final SHA and update PR #13 architecture, runner rationale, direct/remote restart evidence, isolation, benchmark, risks and Parrot guide.
9. [pending] Mark PR #13 ready only after Verify, Race detector, Staticcheck, Govulncheck, CodeQL, Dependency review, Container SBOM and vulnerabilities, Distributed OpenCode E2E and Bubblewrap host isolation are all completed and green on the final SHA.

The local race command is blocked only because the VPS lacks `gcc`; final-SHA GitHub Race remains mandatory. No merge, deployment, pairing, real Parrot installation, tag, Coolify changes, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL cleanup.
