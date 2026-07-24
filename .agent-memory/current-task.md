# Current task

Branch: `p16-global-work-scheduler`. Pull request: `#48`.

Published head before this local fix: `e1bdb5a7468a7385d8cc4b0ee6ce613b4c00154b`.

## Exact-head evidence

`Verify` is green on `e1bdb5a`. `Rootless BuildKit candidate fixture` is the only observed failure so far. Its exact job log proves:

- the private rootless service started under `mcp-build`;
- the complete rootlesskit/buildkitd process subtree stayed in the reviewed cgroup;
- both OCI builds completed;
- the second build emitted `CACHED`;
- failure happened immediately after the second build, before `cache-usage.txt` or `cache-policy.txt` existed.

The cause is the fixture verifying OCI files from the runner identity even though the output directory is private `0700` state owned by `mcp-build`.

## Local fix

The fixture now verifies each OCI artifact as `mcp-build`:

- regular file;
- not a symlink;
- non-empty;
- size greater than zero and no more than 64 MiB.

Only after those checks does it validate `CACHED`, run `buildctl du -v`, and retain the reviewed `maxUsedSpace = "4GB"` policy evidence.

No cache, output, service or isolation bound was weakened.

## Local validation

Green on the exact local tree:

- `go test ./packaging/builder ./docs ./internal/workflowpolicy -count=1`;
- Actionlint v1.7.12;
- `git diff --check`;
- no temporary diagnostic files remain.

Next: commit and publish the isolated identity correction, hold the new SHA stable, require every exact-head check green, then merge only through the reviewed PR path. Real VPS 50/65/80 calibration remains a host-root boundary and must use the committed durable bootstrap for the exact green commit before Step 8.
