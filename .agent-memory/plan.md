# P11.2 execution plan

1. [done] Reconstruct Git and the nested-Docker failure.
2. [done] Add closed, redacted Bubblewrap diagnostics and unit coverage.
3. [done] Add incremental host preflight.
4. [done] Split unprivileged Docker relay from authoritative host Bubblewrap/combined E2E.
5. [done] Remove nested Docker capability/AppArmor/seccomp bypasses and retain fail-closed coverage.
6. [active] Publish the bounded correction after run `29535155313`: separate relay/combined artifacts, enforce identical report trees, remove the Docker-only benchmark from host, and adapt only the test-tagged translated provider boundary.
7. [pending] Obtain green relay container, host Bubblewrap and combined sandbox reports; extract metrics from those artifacts.
8. [pending] Add `docs/install-opencode-edge-parrot.md` and the dated P11.2 baseline/benchmark from green evidence.
9. [pending] Run final local/remote gates on one exact tree, publish `Step 8: isolate OpenCode runtime with Bubblewrap`, update PR #13 and mark ready only after every mandatory check is completed and green.

Current corrected tree passes standard/tagged full tests, Actionlint and diff check. Do not wait indefinitely on CI; inspect exact jobs and checkpoint demonstrated failures.

Hard boundaries: no merge, deployment, pairing, real Parrot installation, tag, Coolify changes, frontend, Goal Runtime, Build Workcell, HTB/THM/VPN or broad historical CodeQL cleanup.
