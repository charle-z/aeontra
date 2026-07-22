# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`.

P16 Step 2 is implemented locally but not deployed. The package now uses closed recover, prepare, health-check, finalize, and rollback state operations as the Edge user. Onboarding reuses a valid identity without another pairing code or opaque ID output. `mcp-edge doctor` and `doctor --repair` use bounded status and the fixed repair service. Documentation is in `docs/install-edge-parrot-p16.md` and `docs/edge-lifecycle-migration.md`.

Verification: `go test ./... -count=1`, `go vet ./...`, component builds, package/onboarding tests, documentation tests, and `git diff --check` pass. The local race test cannot run because CGO is disabled; remote exact-head CI remains required.

Next: commit Step 2, publish the branch without force push, inspect exact-head PR checks, then continue Step 3. Do not alter the real Parrot Edge before remote package/race validation.
