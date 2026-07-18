# P12 — Trusted Linux Workcell

Current local branch is the unpublished legacy branch; rename it to `p12-trusted-linux-workcell` after the Step 6 commit and before publication.
Base: `origin/main` at `087f00e404855cc83e76c1eb7d6ed85ab14577c5`.

Historical deployed foundations preserved:
- P8.1 Console 2.0 is deployed and tagged `p8.1` at `d343264bffdc0ae1bc045a9d723e913be977090c`.
- P9 Brain is deployed at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.
- P11.2 Remote OpenCode Relay remains the sandbox/relay foundation.
- Public catalog remains exactly 85 tools with `sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941`.

Completed commits:
- `03e83bb` — Step 1: trusted workspace metadata and local CLI.
- `901ff9b` — Step 2: durable local context, HTB template and VPN/route preflight.
- `bae1904` — Step 3: isolated Linux workcell policy.
- `07b128f` — Step 4: rootless tools and durable cleanup.
- `8c1a836` — Step 5: document trusted Linux workcell.

Step 6 candidate:
- dated closure baseline `docs/baselines/2026-07-18-p12.md`;
- regression/documentation test preserving one `linux-workcell` profile, default `dev`, optional local `htb-linux`, sandbox no-network behavior, rootless-only containers, strict Linux roots, local HTB metadata, versioned template, and catalog identity;
- actual-process workflow `.github/workflows/trusted-linux-workcell-e2e.yml` for trusted Bubblewrap/shared-network behavior, process-group cancellation, controlled HTB fixture, rootless Podman build/Compose/PostgreSQL/Chromium, cleanup, service restart, and orphan verification;
- rootless client environment now supplies the owning user's HOME and XDG runtime root for Podman API cleanup without exposing them to the VPS;
- no real Parrot installation, pairing, VPN action, or host modification was performed.

Local final verification passed:
- `go fmt ./...`;
- `go test -p 1 ./... -count=1`;
- deterministic grouped atomic coverage combined into one profile, with all package thresholds green;
- `go vet ./...`;
- `go build ./...`;
- Staticcheck `v0.7.0` with a private temporary cache;
- Govulncheck `v1.6.0`: no vulnerabilities found;
- Actionlint `v1.7.12`;
- tagged test binaries for `opencode_e2e` edgeclient, `opencode_e2e` mcpserver, and `p12_e2e` edgeclient;
- `git diff --check`.

Race was not represented as local success; it remains a blocking GitHub Actions job with `CGO_ENABLED=1`.

Next exact action: create `Step 6: close trusted Linux workcell`, confirm no temporary helpers/artifacts, rename the local branch to `p12-trusted-linux-workcell`, publish only that branch, open the PR against `main`, and wait for every exact-head check.
