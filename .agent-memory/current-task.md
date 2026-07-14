# P9 Brain — managed volume root hardening

Status: P9 code is on `main` at `206c36c9732d780e9117ca62cf42a1a087844ea2`. The exact Brain storage is persistent and verified, and `MCP_DEVBOX_BRAIN_ROOT=/brain` is configured. Brain activation deployment `jev18msy9y0umj9z7p1nk31q` failed while the previous container remained healthy with 67 tools and the P9 catalog hash.

Historical release-candidate state retained for traceability: P9 Brain was complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, while preserving the no resident service invariant.

## Failure analysis

- Coolify's public deployment API exposes the failed deployment state and exact commit but not candidate container logs;
- the Docker image creates `/brain` without an explicit chmod, so a newly initialized managed volume can expose an empty root with platform-default mode `0755`;
- Brain intentionally requires root mode `0700`, reproducing the candidate startup failure for a fresh empty managed volume;
- storage and env configuration remain intact; no duplicate deployment, storage POST, DELETE, application, or irreversible action was performed after the failure.

## Implemented on `fix/p9-brain-managed-volume-mode`

- only the exact production managed-volume path `/brain` may use the empty-root hardening path;
- an empty, non-symlink `/brain` root may be changed to `0700` when the process is permitted to chmod it;
- every other Brain root keeps the original strict permission rejection, including an empty broad root;
- a non-empty `/brain` root with mode other than `0700` still fails closed and is left unchanged;
- child directories, existing Brain data, symlink checks, exact private modes and source validation remain unchanged;
- added regression coverage for empty managed-volume hardening, non-empty rejection, exact `/brain` selection and the historical broad-root invariant;
- no public tool, console, OAuth, Edge, workcell, HTB, Docker socket, free terminal, storage mutation, or deployment behavior changed.

## Local verification

- `go fmt ./internal/brain`: clean;
- `go test ./internal/brain -count=1`: pass;
- `go test ./... -count=1`: pass;
- `go vet ./...`: pass;
- `go build ./...`: pass;
- `git diff --check`: pass;
- catalog remains 67 tools.

## Next exact actions

1. Commit the hotfix without force or AI signature.
2. Publish an independent PR, require every check green and merge only its exact SHA.
3. Deploy the merged hotfix once and observe the same deployment ID to terminal state.
4. Verify Brain startup, exact commit, healthy status, 67 tools and P9 hash.
5. Reverify storage idempotently and run catalog plus Brain smokes without private output.
6. Record final evidence and create annotated tag `p9` only after every condition passes.
7. Do not start frontend work before closure and tagging.
