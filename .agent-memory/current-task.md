# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head is `67b535e7cd8c47da3fa591adaec8d60554961bdb`; a rootless E2E correction is local and not yet published.

P16 Step 3 includes durable project aliases, bounded checkout discovery, canonical precedence, explicit ambiguity, and an internal five-minute preview/apply association transaction. It can reuse a canonical checkout or associate one unique clean legacy checkout without moving it, compensates a newly registered workspace if project binding fails, and rejects drift, dirty state, remote mismatch, symlink replacement, root escape and stale/future plans. Clone and public project approval tools remain unfinished.

GitHub Actions diagnostics reuse the VPS/Coolify `GITHUB_TOKEN`, `GITHUB_OWNER`, and `GITHUB_OWNER_TYPE`. `source_pull_request_failure_diagnostics` resolves failed jobs on the exact PR head/latest workflow attempt and returns failed steps, annotations and line-numbered log context. `source_pull_request_job_log` returns a redacted full job log in chunks with `next_offset`, up to 1 MiB per call and a 16 MiB window. Authorization is sent only to the GitHub API; the signed download receives none. No `gh`, new token, Edge credential, job ID or signed URL is required or exposed.

The candidate catalog remains 100 tools with hash `sha256:370a309ba1b63a500dd4d2abae77a11e60e49b35cdbfdc3adf4e692e78772ea2`. Historical P15/production documentation remains correctly fixed at 98 tools until merge and deployment.

Exact-head CI for `67b535e` exposed a real rootless E2E failure: `Rootless Podman, PostgreSQL and Chromium` failed at `stage_postgres` with `not_found`. A temporary token-backed probe using the existing Coolify authority successfully read the exact job, annotations and bounded log without `gh`, proving the new design. Root cause: the workflow exported a raw Docker config digest (`sha256:...`) after loading the archive into Podman; `image exists` accepted it, but `podman run` did not resolve it reliably.

The local correction derives exactly `localhost/p12-postgres-fixture:<64 lowercase hex>` from that verified config digest, tags the already loaded image, verifies with `podman image inspect` that the tag resolves to the same ID, verifies existence, and exports only that closed local reference. The E2E parser rejects registry names, mutable tags, raw digests, uppercase hex and traversal. No network pull or arbitrary image reference is enabled.

Verification after the correction: tagged unit tests, normal `internal/edgeclient`, workflow policy, redactor and docs pass; `go vet ./...`, Staticcheck v0.7.0 and `git diff --check` pass. Local race remains unavailable because CGO is disabled; exact-head CI is required.

Next: update handoff, commit and publish the rootless fixture correction, then inspect PR #48 exact-head gates. Do not touch the real Parrot Edge or production before all gates are green.
