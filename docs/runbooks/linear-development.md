# Runbook: develop and recover a project

Use human project and Edge aliases throughout. Configuration and authority remain
defined by [configuration](../configuration.md), [security](../security.md) and
the [tool contract](../tools.md). This workflow does not grant new access.

## Find the workspace

1. Read `edge_onboarding_status` or `edge_bundle_status` with `target`.
   Legacy clients may supply `device_id` instead; never supply both.
2. Read `project_registry_list(target)` and select the intended project alias.
   A project absent on one target may be registered on another authorized target.
3. Read `project_status(alias,target)` and `project_git_status(alias,target)`.
   Normal dirty state is allowed. Reconcile a diagnosed identity problem before
   writing; do not clone over existing work or release a claim speculatively.

## Start a new repository

Prepare the owner-bound project with `project_prepare`. Read `project_snapshot`:
an empty repository reports `unborn=true` and no commit. Use `project_exec` or
the project's toolbox to create files, run tests and make the first local commit.
Git publication remains a separate reviewed operation.

Managed task worktrees need a committed base. `project_task_start` on an unborn
repository returns `initial_commit_required` and points to `project_exec`; it does
not invent a base, start workers, or discard files. Durable capability-driven
command inspection currently also requires a committed source baseline. Use the
direct workcell/toolbox path until that first commit exists.

## Run and recover work

- Foreground commands: `project_exec` returns the operation ID and bounded command
  result. A timeout or lost response is not a passing exit. Inspect the recorded
  operation before considering an exact retry with the original idempotency key.
- Durable exact commands: start with `project_development_start`; observe with
  `project_development_status`. If the request ID is lost, call
  `project_development_list(alias,target)` and select the request. Its read-only
  inventory never dispatches or retries work. It includes awaiting-reasoning and
  retained terminal records. `list_complete=false` means the bounded list is not
  exhaustive; increase `limit` up to 100 rather than concluding older work vanished.
- Model tasks: recover with `project_task_list`, then read `project_task_status`
  and follow its current continuation. A completed runtime is not necessarily an
  accepted objective, and the server cannot resume a closed ChatGPT turn itself.
- Processes: `project_process_list` always returns a `processes` array. An empty
  array in a succeeded operation means no matching retained processes, not a
  failed command with exit zero. Inspect logs and stop only the captured process.
- Browser harnesses: recover through `project_browser_harness_list` and status;
  do not start a second server just because the first response was lost. A port
  conflict is command evidence, not permission to stop unrelated services.

Snapshot failures preceding a task return `reason`, `operation_id`, operation
state and `next_tool`. `snapshot_pending` points to `edge_operation_status` for
the original operation; repository/binding failures point to `project_status`.
These diagnostics do not authorize replay or automatically reconcile a workspace.

## Read results and publish

Large results are envelopes, not source files. Follow `result_ref` using
`result_read` or `result_stage` and the returned offsets until complete. Never
rebuild Git objects from redacted output. Brain accepts exact slugs in
`brain_search`; use `brain_read` for the complete selected note.

Review the actual diff, run the documented gates, make a focused commit, then use
publication preview/execute. Check exact-head CI: `evidence_complete=false` means
unknown or incomplete evidence, even if totals are zero. `action_required`
requires upstream action and cannot be declared green or bypassed.

If a tool or field exists in the server catalog but not in the client, follow
[catalog refresh](catalog-cache.md). Repeated deployments cannot replace a
conversation's already loaded schema.
