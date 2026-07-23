# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Historical deployed successor truth remains explicit: P8.1 is deployed at `d343264bffdc0ae1bc045a9d723e913be977090c`, and P9 Brain is deployed as its successor at `4fbe1dda02351c632e67c0f10a5c5b314df745e2`.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Current published candidate head is `3982b93e2a659a452ebc0ea8b7f4ed687dcb96fd`; its first exact-head Verify run exposed only this historical-documentation omission.

## P16 Step 3 candidate implemented

Alias-first public tools:

- `project_prepare(alias, repository, target)`
- `project_status(alias, target)`

The public schemas accept no path, URL, owner, branch, credential, device/workspace/operation/runtime/job/plan ID. A human target such as `parrot` must resolve to exactly one active paired Edge; absence or duplicate active names fail closed.

`project_prepare` executes one signed closed Edge operation and locally plans/revalidates one of:

- `reuse_existing`;
- `associate_existing` for exactly one clean owner-bound legacy checkout;
- `clone` into the inferred canonical direct child.

Clone uses only the existing private Edge GitHub credential and fixed runner. It creates the canonical directory exclusively with mode 0700, keeps an open descriptor for the reservation, runs a closed owner-bound `git clone --single-branch -- URL .`, verifies `.git`, fetch/push remotes and clean state, then registers the workspace/project. Ordinary failure removes only the exact reserved directory. A replaced path/inode is preserved with `cleanup_required`. Crash after successful clone converges through later discovery instead of blind reclone.

Stable new project codes: `credential_unavailable`, `clone_failed`, `cleanup_required`.

The signed operation result contains an internal workspace ID only for control-plane workspace synchronization. The public project view returns only alias, owner/repository, target, state, profile, mode and safe reason.

## Final review hardening

Two real issues found during final review were fixed and covered by tests:

1. `project_prepare` may contact GitHub for clone, so its MCP annotation honestly uses `openWorldHint=true` (`0/0/1/1`).
2. Project operation completions reject any unrelated mixed metadata. Validation strips only the expected project/workspace fields and requires every remaining `OperationResult` field to be empty.

Current deterministic catalog: 102 tools, hash `sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b`. Historical catalog evidence remains filtered and unchanged.

## Local validation

Green before publication:

- all Go packages, run in bounded groups;
- focused `internal/edgeclient`, `internal/edge`, `cmd/mcp-edge`, `internal/mcpserver`, app/integration/docs/catalog packages;
- tagged compile gates for `p12_e2e` and `opencode_e2e`;
- `go vet ./...`;
- Staticcheck v0.7.0 with a temporary writable cache;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`;
- no temporary helper/probe files remain.

Exact Verify reproduction showed that implementation packages passed and only the current-task historical phrases were missing. After restoring them:

- all documentation tests pass;
- the 14 package-specific atomic coverage thresholds pass, including `internal/mcpserver` at 81.7%, `internal/brain` at 80.6%, `internal/taskjournal` at 80.8%, and `internal/tools` at 74.7%;
- the monolithic local coverage command is still terminated by the connector after long execution, so profiles were generated in bounded groups and combined for the identical coverage-gate evaluation.

Local race remains unavailable because CGO is disabled; exact-head CI is authoritative.

Next: commit this minimal documentation correction, publish it to PR #48, hold the new SHA stable, and require every exact-head gate green. Do not touch real Parrot, Coolify production or `main` before the matrix is green.
