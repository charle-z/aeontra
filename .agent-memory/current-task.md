# Current task

Historical deployed baseline remains unchanged: VPS `main`/production truth is preserved separately; no production, Coolify, Parrot or `main` mutation occurred in this cut.

Branch: `p16-global-work-scheduler`. Pull request: `#48`. Published exact head before this local cut: `2bb4cf5ccbe387272dc5ec16d16dca5168ac259d`, which passed 15/15 checks.

## P16 Step 3 candidate implemented locally

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

1. `project_prepare` may contact GitHub for clone, so its MCP annotation is now honestly `openWorldHint=true` (`0/0/1/1`).
2. Project operation completions now reject any unrelated mixed metadata. Validation strips only the expected project/workspace fields and requires every remaining `OperationResult` field to be empty.

Current deterministic catalog: 102 tools, hash `sha256:5a2091d85585d13eb7efbc22d942b2dfbd71fc7d547581803eb7633cac64d68b`. Historical catalog evidence remains filtered and unchanged.

## Local validation

Green:

- all Go packages, run in bounded groups;
- focused `internal/edgeclient`, `internal/edge`, `cmd/mcp-edge`, `internal/mcpserver`, app/integration/docs/catalog packages;
- tagged compile gates for `p12_e2e` and `opencode_e2e`;
- `go vet ./...`;
- Staticcheck v0.7.0 with a temporary writable cache;
- `go build ./...`;
- Actionlint v1.7.12;
- `git diff --check`;
- no temporary helper/probe files remain.

Local race remains unavailable because CGO is disabled; exact-head CI is authoritative.

Next: write handoff, commit this isolated Step 3 cut, publish the same branch/PR, hold the new SHA stable, and require every PR #48 gate green. Diagnose failures from exact-head Actions logs. Do not touch real Parrot, Coolify production or `main` before the matrix is green.
