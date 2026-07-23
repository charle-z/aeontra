# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head before this local cut is `1e987a23b362ead9a0923493e298a9e96fb416c2`; the working tree contains two exact-head CI fixes ready to commit and publish.

P16 Step 3 currently includes durable project aliases, bounded checkout discovery, canonical precedence, explicit ambiguity, internal preview/apply association, and GitHub Actions diagnostics using the existing Coolify GitHub authority. `source_pull_request_failure_diagnostics` returns failed step, annotations and bounded context. `source_pull_request_job_log` returns the redacted full job log in chunks with `next_offset`, without requiring `gh`, another token, job IDs or signed URLs.

Exact-head CI at `1e987a2` exposed two real failures and both were diagnosed through the existing `GITHUB_TOKEN`, `GITHUB_OWNER`, and `GITHUB_OWNER_TYPE` authority:

1. Debian package migration failed with `rename_failed`. The journal was created successfully, but the container filesystem rejected `RENAME_NOREPLACE` for the directory move. The local fix keeps `RENAME_NOREPLACE` as the primary operation and, only for unsupported directory flags, creates a private exclusive empty placeholder and performs atomic `RENAME_EXCHANGE`. It verifies the placeholder inode and emptiness before removing it. Files do not use the fallback. Existing destinations are never overwritten, cross-filesystem moves remain rejected, and no file-by-file copy occurs. Safe error categories now distinguish `unsupported`, `cross_device`, `permission`, `destination_conflict`, `path_missing`, and `busy` without exposing paths.

2. Rootless PostgreSQL failed before E2E cycles because the archive was saved under the normal Docker name and Podman was later asked to reconstruct/tag from a raw config digest. The local fix obtains the immutable config digest in Docker, creates the closed local reference `localhost/p12-postgres-fixture:<64 lowercase hex>`, verifies it resolves to the same ID, saves the archive with that reference, loads it into Podman, verifies the preserved local tag and normalized ID, and exports only that reference. No runtime pull or arbitrary image reference is enabled.

Local verification after both fixes:
- all Go packages pass in bounded groups;
- `internal/edge`, `internal/edgeclient`, `internal/edgelifecycle`, package, workflow-policy, docs and MCP tests pass;
- P12 tagged rootless tests pass;
- `go vet ./...` passes;
- Staticcheck `v0.7.0` passes with a writable temporary cache;
- all main binaries build;
- `actionlint@v1.7.12` passes;
- `git diff --check` passes.

Local race remains unavailable because CGO is disabled. Exact-head CI race and the real Debian/rootless jobs remain mandatory.

Next: commit the two fixes as an isolated Step 3 stabilization commit, publish the branch, require all PR #48 checks green, diagnose any remaining failure through the new GitHub log path, and only then continue Step 3 clone/public project tools. Do not touch the real Parrot Edge, Coolify production, or `main` directly before green exact-head gates.
