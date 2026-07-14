# P9 Brain — Coolify preview environment projection fix

Status: P9 Brain is deployed and healthy at `e7ffdcf781aee2cacc092412a32fd9205cebbeee` through deployment `a81t5q7b7uqbw2m8xoyj1rc5`. Production reports 67 tools and catalog hash `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`. `mcp-catalog-smoke` and `brain-smoke` passed with index ready, schema version 1, zero initial notes and zero context bytes.

Historical release-candidate state retained for traceability: P9 Brain was complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, while preserving the no resident service invariant.

## Final environment observation

- the exact storage is persistent, unique and mounted at `/brain`;
- `MCP_DEVBOX_BRAIN_ROOT=/brain` exists and Brain starts successfully;
- Coolify exposes the same logical environment key twice: one production entry with `is_preview=false` and one preview projection with `is_preview=true`, created at the same time;
- the final idempotent verification correctly stopped before writing because the previous implementation treated the preview projection as a second production key;
- no variable, storage, deployment, application or data was deleted or duplicated by that stopped call.

## Implemented on `fix/p9-coolify-env-preview`

- parse the Coolify `is_preview` flag for environment entries;
- ignore preview projections only when determining production-key uniqueness and deciding POST versus PATCH;
- preserve conflicts for more than one non-preview production entry;
- preserve POST for a missing production key and PATCH for exactly one production key;
- never delete or mutate the preview projection directly;
- add regression coverage for one production entry plus one preview projection, while existing tests retain duplicate-production conflict coverage;
- no public tool, console, OAuth, Edge, workcell, HTB, Docker socket, terminal, storage or deployment behavior changed.

## Local verification

- `go fmt ./internal/tools`: clean;
- `go test ./... -count=1`: pass;
- `go vet ./...`: pass;
- `go build ./...`: pass;
- `git diff --check`: pass;
- production-plus-preview regression test: pass;
- catalog remains 67 tools.

## Next exact actions

1. Commit and publish the hotfix without force.
2. Open an independent PR, require every check green and merge only its exact SHA.
3. Deploy the merged hotfix once and observe the same deployment ID to terminal state.
4. Reverify storage/env idempotently, then rerun catalog and Brain smokes against the exact merged commit.
5. Create annotated tag `p9` only after every condition passes.
6. Do not start frontend work before closure and tagging.
