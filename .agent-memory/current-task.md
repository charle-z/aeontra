# P9 Brain — Coolify storage response hotfix

Status: PR #5 was merged to `main` at `67d297b53c2c7984ffe2ad25183b442e583cb86a` and deployed once as `m21e5djfl620i2955chxsjht`. That runtime is healthy with 67 tools and catalog hash `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`. Brain remains disabled.

Historical release-candidate state retained for traceability: P9 Brain was complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, while preserving the no resident service invariant.

## Observed production mismatch

- the first exact `coolify_set_env` call for `MCP_DEVBOX_BRAIN_ROOT=/brain` failed before any write with `unexpected Coolify collection response`;
- direct read-only inspection showed Coolify v4 returns storages as `{persistent_storages:[...], file_storages:[...]}` rather than a direct array or `{data:[...]}`;
- no storage, env variable, deployment, application, or deletion was produced by the failed call.

## Implemented on `fix/p9-coolify-storage-response`

- added a storage-specific decoder that preserves direct-array and `{data:[...]}` compatibility;
- normalizes `persistent_storages` entries to `type=persistent` and `file_storages` entries to `type=file` before existing exact-name/mount conflict checks;
- added a regression test using the exact grouped response shape observed from production;
- no public tool, console, OAuth, Edge, workcell, HTB, Docker socket, terminal, application, or deployment behavior changed.

## Local verification

- `go fmt ./internal/tools`: clean;
- `go test ./... -count=1`: pass;
- `go vet ./...`: pass;
- `go build ./...`: pass;
- `git diff --check`: pass;
- grouped Coolify v4 storage regression test: pass;
- catalog remains 67 tools.

## Next exact actions

1. Commit and publish the hotfix branch without force.
2. Open an independent PR, require all checks green, and merge only the exact checked SHA.
3. Deploy the merged hotfix once and observe the same deployment ID to terminal state.
4. Retry the exact `coolify_set_env` Brain configuration so storage is verified or created before env mutation.
5. Deploy Brain once and observe that same deployment ID to terminal state.
6. Verify exact commit, healthy status, 67 tools, P9 hash, storage persistence, catalog smoke and Brain smoke.
7. Record final production evidence and create annotated tag `p9` only after every condition passes.
8. Do not start frontend work before closure and tagging.
