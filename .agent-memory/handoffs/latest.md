# Latest handoff — P12 Trusted Linux Workcell closure candidate

Date: 2026-07-18
Final branch before publication: `p12-trusted-linux-workcell`.
Current HEAD before Step 6: `8c1a836fcaf33a10e9d7007b45a947c9c118e1f4`.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Historical foundations remain explicit:
- P8.1 is closed, deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- Its historical catalog had 67 tools and Edge state `not_paired`.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P11.2 Remote OpenCode Relay remains the sandbox and relay foundation.
- The catalog remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed commits:
- `03e83bb` — Step 1: trusted workspace metadata and local CLI.
- `901ff9b` — Step 2: durable local context, HTB template and VPN/route preflight.
- `bae1904` — Step 3: isolated Linux workcell policy.
- `07b128f` — Step 4: rootless tools and durable cleanup.
- `8c1a836` — Step 5: document trusted Linux workcell.

Step 6 closure candidate contains:
- `docs/baselines/2026-07-18-p12.md` and `docs/p12_closure_test.go`;
- roadmap and documentation synchronization;
- `.github/workflows/trusted-linux-workcell-e2e.yml` with actual-process trusted host/HTB and rootless Podman/PostgreSQL/Chromium jobs;
- tagged P12 E2E tests and bounded reports;
- Podman client environment support for runtime-labelled cleanup;
- Staticcheck-compliant P12 errors without unrelated historical churn.

Final local gates passed:
- format;
- full serial suite;
- deterministic grouped atomic coverage with all thresholds passing;
- vet and build;
- Staticcheck v0.7.0;
- Govulncheck v1.6.0 with no vulnerabilities;
- Actionlint v1.7.12;
- tagged OpenCode and P12 E2E test binary builds;
- diff whitespace check.

Race remains intentionally pending as a blocking GitHub CGO job. Actual P12 E2E processes also remain pending until GitHub executes the new workflow. No real Parrot host was modified.

Next: create `Step 6: close trusted Linux workcell`, confirm a clean tree, rename the unpublished local branch, publish only `p12-trusted-linux-workcell`, open the PR, and inspect every exact-head check before merge.
