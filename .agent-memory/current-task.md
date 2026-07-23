# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Current published head is `0929c784a378a898ff6106c3d32c703a3027a2b3`; one additional rootless correction is local and validated.

P16 Step 3 includes durable project aliases, bounded checkout discovery, preview/apply association, and GitHub Actions diagnostics using the existing Coolify `GITHUB_TOKEN`, `GITHUB_OWNER`, and `GITHUB_OWNER_TYPE`. `source_pull_request_failure_diagnostics` returns failed steps, annotations and context; `source_pull_request_job_log` returns the redacted full log in chunks without `gh`, another token, job IDs or signed URLs.

Exact-head CI at `0929c78` proved the Debian migration fix: `Edge, autopilot, updater, and migration` passed, and the signed Debian package progressed without the prior `rename_failed`. The remaining Rootless failure was read through the newly implemented log path using the existing GitHub authority.

The full Rootless log proves:
- `podman load` completed and stored the PostgreSQL image;
- manifest parsing completed;
- the job exited at the first command after local-reference derivation: remote `podman --url ... image exists`;
- it never reached `image inspect` or the E2E cycles.

Root cause: `image exists` is not reliable through the rootless Podman service/remote client used by this gate. The local correction removes that command everywhere. Workflow setup now calls `image inspect --format {{.Id}}`, emits stable `postgres_image_inspect` or `postgres_image_identity` categories, and compares the normalized ID with the archive config digest. Each PostgreSQL E2E cycle also calls `image inspect` and requires the exact digest encoded in the closed local reference before creating resources or running a container.

Local verification for this correction:
- focused rootless/workflow/docs tests pass;
- tagged P12 Edge-client tests pass;
- full `go test ./... -count=1` passes;
- `go vet ./...` passes;
- Staticcheck v0.7.0 passes with a temporary writable cache;
- `go build ./...` passes;
- actionlint v1.7.12 passes;
- `git diff --check` passes;
- no temporary probes remain.

Local race remains unavailable because CGO is disabled. Exact-head CI remains mandatory.

Next: commit this rootless remote-inspection correction, publish it, hold the new SHA stable, and require all PR #48 checks green. Diagnose any further failure through the Actions diagnostics tools. Do not touch the real Parrot Edge, Coolify production, or main directly before green exact-head gates.
