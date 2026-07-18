# Plan — P12 Trusted Linux Workcell closure

1. Preserve the completed one-profile contract: `linux-workcell`, default `dev`, optional local `htb-linux`; keep P11.2 `sandbox` networkless and fail-closed.
2. Close Step 6 with dated baseline, regression documentation tests, actual-process P12 E2E workflow, rootless client compatibility, and complete local gates.
3. Commit exactly `Step 6: close trusted Linux workcell` without squashing or rewriting Steps 1–5.
4. Remove ignored/local coverage output and confirm no `tmp_*`, scratch, private artifact, or generated E2E report is in the tree.
5. Rename the unpublished local branch to `p12-trusted-linux-workcell`; do not create an old-name remote branch.
6. Publish only the final branch and open a PR against `main` titled without the former product name.
7. Read all check runs for the exact PR head. Required coverage includes Verify, Responsive Brain graph, Race detector, Staticcheck, Govulncheck, CodeQL, Dependency review, container SBOM/vulnerability gate, distributed OpenCode, Bubblewrap host isolation, trusted host/HTB fixture, and rootless Podman/PostgreSQL/Chromium.
8. Correct only real causes on this branch. Do not relax assertions, skip viewports, lower security thresholds, use rootful Docker, or weaken sandbox/network/filesystem controls.
9. Merge by merge commit only when the exact head is fully green.
10. Wait for the existing automatic deployment; do not trigger a duplicate deployment.
11. Verify production serves the exact merge commit, remains `running:healthy`, exposes exactly 85 tools and the unchanged catalog hash, and passes catalog, Brain, console, authentication, and P11.2 sandbox/not-paired smokes.
12. Report the exact final branch, commits, PR, final SHA/tree, merge commit, deployment, gates, shared-network boundary, rootless evidence, wordlists, HTB fixture/template, local memory, ordered Parrot setup, and residual risks. Do not install or pair the real Parrot machine.
