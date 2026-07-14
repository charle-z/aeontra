# P9 Brain — Coolify physical storage name hotfix

Status: PR #6 was merged to `main` at `a976334a6e7d8c8c68efe50cc3832b51ae9628a0` and deployed once as `g8t2ftj98z8u77g466heu4i0`. That runtime is healthy with 67 tools and catalog hash `sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed`. Brain remains disabled.

Historical release-candidate state retained for traceability: P9 Brain was complete / merge-ready on branch `p9-brain`, based on P8 closure `2e3429c9d6342e8e091cadf65293c5c85b1b3259`, while preserving the no resident service invariant.

## Observed production behavior

- the exact `coolify_set_env` workflow created the requested managed persistent storage with mount `/brain` and no `host_path`;
- Coolify canonicalized the physical volume name from logical `mcp-devbox-brain` to `jqf7qz5ensoqtvl1tb197gcv-mcp-devbox-brain`;
- reverification rejected that platform-owned prefix as a conflict, so the env variable was not written;
- the existing volume is persistent, unique, mounted at `/brain`, and no DELETE or duplicate POST was attempted after discovery.

## Implemented on `fix/p9-coolify-storage-name`

- normalize only the exact fixed application prefix `jqf7qz5ensoqtvl1tb197gcv-` from returned physical storage names;
- retain exact logical name `mcp-devbox-brain`, persistent type, mount `/brain`, duplicate, reserved-name, and reserved-path conflict checks;
- add regression coverage using the exact prefixed name observed in production;
- no public tool, console, OAuth, Edge, workcell, HTB, Docker socket, terminal, application, delete path, or deployment behavior changed.

## Local verification

- `go fmt ./internal/tools`: clean;
- `go test ./... -count=1`: pass;
- `go vet ./...`: pass;
- `go build ./...`: pass;
- `git diff --check`: pass;
- physical-name normalization regression test: pass;
- catalog remains 67 tools.

## Next exact actions

1. Commit and publish the hotfix without force.
2. Open an independent PR, require all checks green, and merge only the exact checked SHA.
3. Deploy the merged hotfix once and observe the same deployment ID to terminal state.
4. Retry the exact `coolify_set_env` Brain configuration; it must recognize the existing persistent volume idempotently and then create or update the env.
5. Deploy Brain once and observe that same deployment ID to terminal state.
6. Verify exact commit, healthy status, 67 tools, P9 hash, storage persistence, catalog smoke and Brain smoke.
7. Record final production evidence and create annotated tag `p9` only after every condition passes.
8. Do not start frontend work before closure and tagging.
