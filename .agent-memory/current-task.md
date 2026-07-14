# P9 Brain — Coolify production closure fix

Status: P9 is merged to `main` at `1faddafd866c426edf5e76d4d336d0b2b7d3f2b6`. Work continues on independent branch `fix/p9-coolify-storage-env`; production deployment and the annotated `p9` tag remain pending.

Historical release-candidate state retained for traceability: P9 Brain was complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, while preserving the no resident service invariant. The current fix branch does not reopen or alter that P9 implementation scope.

## Implemented on the fix branch

- fixed `coolify_set_env` to list existing env variables first, use POST only for missing keys, PATCH the same application `/envs` endpoint for unique existing keys identified by `key`, reject duplicate-key conflicts before writes, and never return submitted values or response bodies;
- validated the production Coolify v4 contract directly: `PATCH /api/v1/applications/{app}/envs` with `{key,value}` returned HTTP 201, while `/envs/{env_uuid}` returned 404 and UUID in the payload was rejected;
- added a fixed internal P9 Brain storage helper without adding a public tool or privileged-profile dependency;
- the helper is reachable only inside the existing `coolify_set_env` workflow when the application is exactly `jqf7qz5ensoqtvl1tb197gcv`, the key is `MCP_DEVBOX_BRAIN_ROOT`, and the value is exactly `/brain`;
- storage handling always GETs first, accepts exactly one matching persistent entry, rejects reserved-name/path/type/duplicate conflicts, POSTs only when absent, omits `host_path`, GETs again to verify, and never calls DELETE;
- unsafe Brain application/path combinations are rejected before HTTP, and a storage conflict stops before any env read or write;
- no console, OAuth, Edge, workcell, HTB, free terminal, Docker socket, new application, privileged profile, or catalog registration changed.

## Local verification

- `go fmt ./internal/tools`: clean;
- `go test ./... -count=1`: pass;
- `go vet ./...`: pass;
- `go build ./...`: pass;
- `git diff --check`: pass;
- directed storage helper, env create/update/redaction, and Brain ordering/rejection/conflict tests: pass;
- public P9 catalog remains 67 tools because no catalog registration changed.

## Next exact actions

1. Commit this fix branch without AI signature.
2. Publish without force and open an independent PR.
3. Require all remote checks green, then merge without non-fast-forward conflict.
4. Deploy the merged fix once with Brain still disabled, retaining and observing that deployment ID to terminal state.
5. Invoke the exact `coolify_set_env` Brain configuration so storage is verified before the env is created or updated.
6. Deploy Brain once, retain and observe that deployment ID, then verify exact commit, health, 67 tools, P9 hash, logs, catalog smoke and Brain smoke.
7. Record production evidence and create annotated tag `p9` only after every check passes.
8. Do not start frontend work before closure and tagging.
