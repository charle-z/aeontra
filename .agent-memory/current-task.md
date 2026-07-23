# Current task

Historical deployed baseline preserved for documentation guards: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published head before this local cut is `e088a884f032efce59066b2bcc54eda49838ffaf`.

P16 Step 3 includes durable project aliases, bounded checkout discovery and association, plus GitHub Actions diagnostics using the existing Coolify `GITHUB_TOKEN`, `GITHUB_OWNER`, and `GITHUB_OWNER_TYPE`. No `gh`, extra token, job ID or signed URL is required from the operator.

Exact-head `e088a88` produced two failures. Both were read through the new diagnostics/full-log path:

1. Signed Debian package: migration reported `rename_failed (permission)`. The fixture created `/home/charles/.config/mcp-devbox-edge` with the final directory owned by `charles`, but the intermediate `.config` directory was created by root. Migration correctly runs as `charles` and cannot remove an entry from a root-owned parent. This was a faulty fixture, not a reason to weaken migration. The workflow now creates `.config` first as `charles`, then the state directory. A test locks ownership and ordering. Documentation states that a root-owned synthetic parent remains a permission failure rather than being silently repaired.

2. Rootless PostgreSQL: the log proved `podman load` completed, but the expected Docker archive tag was not present in the remote Podman service. Both `image exists` and direct `image inspect` of the assumed tag therefore failed. The workflow now lists rootless images with `images --no-trunc`, writes a bounded temporary inventory, strictly accepts only full lowercase image IDs, requires exactly one match for the archive config digest, and tags only that matched loaded ID with the closed local reference. Existing correct tags are reused; conflicting tags fail. The final tag is inspected and its normalized ID must equal the archive digest. Stable categories cover inventory, loaded ID, conflict, tag, inspect and identity failures. Cleanup removes the temporary inventory. No network pull or arbitrary image reference is enabled.

Validation after both changes:
- focused Debian/workflow/Edge-client/docs tests pass;
- tagged P12 Edge-client tests pass;
- full suite passed through `internal/edge`, then remaining packages passed in bounded groups after the global process hit the workcell time limit;
- `go vet ./...` passes;
- Staticcheck v0.7.0 passes with a temporary writable cache;
- `go build ./...` passes;
- actionlint v1.7.12 passes;
- `git diff --check` passes;
- no temporary probes remain.

Local race remains unavailable because CGO is disabled; exact-head CI remains mandatory.

Next: commit this fixture correction, publish it, hold the new SHA stable, and require PR #48 Debian and Rootless gates to pass. Diagnose any remaining failure with the implemented log tools. Do not touch the real Parrot Edge, Coolify production or `main` before exact-head gates are green.
