# Runbook: develop and recover a project

Use human project and Edge aliases throughout. Configuration and authority remain
defined by [configuration](../configuration.md), [security](../security.md) and
the [tool contract](../tools.md). This workflow does not grant new access.

## Find the workspace

1. Read `edge_connectivity_status(target)` first when connectivity is uncertain.
   This reads server metadata immediately without queueing an operation. A recent
   authenticated contact does not prove capacity or readiness; no recent contact
   can mean shutdown, suspended WSL, network failure or a stopped daemon. `unknown`
   means no usable observation, not an offline diagnosis. Do not enqueue repairs
   or repeat commands merely because a target has no recent contact.
2. Read `edge_onboarding_status` or `edge_bundle_status` with `target` when a
   device-side health probe is needed; unlike connectivity, these await the Edge.
   Legacy clients may supply `device_id` instead; never supply both.
3. Read `project_registry_list(target)` and select the intended project alias.
   A project absent on one target may be registered on another authorized target.
4. Read `project_status(alias,target)` and `project_git_status(alias,target)`.
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

### Isolate independent direct writers

For parallel chats or an experiment that should leave the canonical source intact:

1. Read `project_snapshot` and select its exact committed HEAD.
2. Call `project_workspace_prepare(alias,target,base_commit,idempotency_key)` with a
   fresh key for this independent writer. Save the key and returned `worktree_id`.
3. Pass that id to `project_exec`, `project_process_start`, toolbox operations or Git
   status/fetch/publication. Ordinary source edits and branch changes are allowed.
   Background process status/stop still use the captured `process_id`.
4. After interruption, reuse the original preparation key/base or list recent spaces
   with `project_workspace_status(alias,target)`. Do not create another clone or rerun
   a command before reading its original durable operation result.
5. Use `MCP_DEVBOX_SCRATCH_ROOT` for disposable logs/results and
   `MCP_DEVBOX_ARTIFACT_ROOT` for retained evidence. Status exposes bounded logical
   byte counts; `complete=false` is partial accounting, not an empty directory.
6. Stop/close retained resources through their own tools. Preview exact workspace
   status, then call `project_workspace_cleanup` with that HEAD and a durable key.
   It refuses ignored files, dirty source, detached heads and live references.
   Clean committed work stays on its verified branch, even before publication.
   Preserve/close the remaining items instead of using reset/prune.

Omitting `worktree_id` retains the established canonical-project workflow. The new
selection is Linux-only and rejects leased model worktrees. Its managed branch anchor
is separate from the current branch reported by `project_git_status`.

- Foreground commands: `project_exec` returns the operation ID and bounded command
  result. A timeout or lost response is not a passing exit. Inspect the recorded
  operation before considering an exact retry with the original idempotency key.
- Durable exact commands: start with `project_development_start`; observe with
  `project_development_status`. If the request ID is lost, call
  `project_development_list(alias,target)` and select the request. Its read-only
  inventory never dispatches or retries work. It includes awaiting-reasoning and
  retained terminal records. `list_complete=false` means the bounded list is not
  exhaustive; increase `limit` up to 100 rather than concluding older work vanished.
  The exact plain `pwd` command does not require unrelated repository compilers;
  caller-supplied requirements still apply. Wrapped/local commands, environment
  overrides and external-runner requests retain conservative source inference.
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

## Clean up a completed task

Review the terminal task and its evidence, then use `project_task_cleanup`.
It preserves branches and durable receipts. On Linux it also reclaims the exact
temporary workspace's runtime and cache when no active process/model journal,
toolbox or browser session retains them. Populated artifact roots are preserved.
Clean up any retained toolbox or browser session before task cleanup when it
should also be discarded. This operation does not stop resources implicitly.
Ordinary project caches are unchanged. Stop or explicitly clean up retained
resources through their own tools; never substitute a global engine or cache prune.
Uncertain storage is retained with `worktree storage cleanup deferred` and a
classified reason in Edge logs; it does not prevent recording a completed cleanup.

For deployment observation, `platform_app_status` returns metadata only. Fetch
bounded logs with `platform_app_logs` when needed. The coordinator status tool
reports the published operation's kind, state, phase and revision for both
topology transitions and backend catalog rollouts.

Linked workspaces retain normal local Git commands in both the trusted workcell and
toolbox. If an older linked toolbox lacks its metadata mount, inspect status and use
the existing explicit `project_toolbox_repair` with the same `worktree_id`; reconciliation
preserves its rootfs and source. Canonical toolboxes do not need recreation.
