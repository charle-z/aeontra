# P11.2 execution plan

1. [done] Reconstruct Git and the nested-Docker failure.
2. [done] Add closed, redacted Bubblewrap diagnostics and unit coverage.
3. [done] Add incremental host preflight.
4. [done] Split unprivileged Docker relay from authoritative host Bubblewrap/combined E2E.
5. [done] Remove nested Docker capability/AppArmor/seccomp bypasses and retain fail-closed coverage.
6. [done] Prove host Bubblewrap preflight, isolation and combined OpenCode relay on Ubuntu 22.04.
7. [active] Publish the structured JSON path-translation fix for the Docker-only adapter and validate all three report modes on one tree.
8. [pending] Extract green A/B/C and Bubblewrap startup metrics; create the dated P11.2 baseline.
9. [pending] Run the complete final local/remote gate matrix, publish `Step 8: isolate OpenCode runtime with Bubblewrap`, update PR #13 and mark it ready only after every mandatory check is completed and green.

The current pending tree includes the Parrot WSL2 installation guide and passes standard/tagged full tests, the focused regression, Actionlint and diff check.

Hard boundaries: no merge, deployment, pairing, real Parrot installation, tag, Coolify changes, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL cleanup.
