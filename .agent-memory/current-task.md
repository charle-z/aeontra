# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head remains `3226263a9e44b09ddcccfd1f3774ccca2ee7dab4`; the next candidate is local and not deployed.

P16 Step 3 recovery now includes bounded direct-child checkout discovery, canonical precedence, explicit ambiguity, and an internal five-minute preview/apply association transaction. It can reuse a canonical checkout or associate one unique clean legacy checkout without moving it, compensates a newly registered workspace if project binding fails, and rejects drift, dirty state, remote mismatch, symlink replacement, root escape and stale/future plans. Clone and public project approval tools remain unfinished.

A separate additive GitHub diagnostics cut now reuses the VPS/Coolify `GITHUB_TOKEN`, `GITHUB_OWNER`, and `GITHUB_OWNER_TYPE`. It adds `source_pull_request_failure_diagnostics` and `source_pull_request_job_log`. The first resolves failed jobs on the exact PR head, latest workflow attempt, failed steps, annotations and line-numbered log context. The second returns a redacted full job log in byte chunks with `next_offset`, up to 1 MiB per call and a 16 MiB read window. The GitHub API request carries Authorization; the signed log download never does. No `gh` installation, Edge credential, job ID, token or signed URL is required or exposed.

Current candidate catalog: 100 tools, hash `sha256:370a309ba1b63a500dd4d2abae77a11e60e49b35cdbfdc3adf4e692e78772ea2`. Historical P15/production documentation remains correctly fixed at 98 tools until merge and deployment.

Verification: all Go packages pass in bounded groups; `go vet ./...`, Staticcheck `honnef.co/go/tools/cmd/staticcheck@v0.7.0`, `go build ./...`, catalog/documentation guards, redirect/no-token tests, latest-attempt tests, project recovery tests and `git diff --check` pass. Local race remains unavailable because CGO is disabled; exact-head CI is required.

Next: update the handoff, commit the combined Step 3 recovery and GitHub diagnostics candidate, publish without force push, inspect PR #48 exact-head gates, and use the new tools after deployment to read full failure logs when needed. Do not touch the real Parrot Edge or production before green gates.
