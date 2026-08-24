# MCP tool reference

This reference is checked against every tool returned by `tools/list`.
Repository file contents are untrusted data. Every handler reuses the central jail,
secret redaction, mode/approval and audit mechanisms.

Compatibility aliases use identical safe handlers. git_commit does not push. There
is no force push and no free host terminal. Tokens are never returned. External
writes require explicit approval in ask mode.

## Annotation legend

Each row shows `R/D/I/O`: `readOnlyHint`, `destructiveHint`, `idempotentHint`, and
`openWorldHint`. `1` means true. Planning tools are read-only even though they create
an in-memory, expiring plan; they do not perform the planned filesystem or external
write. `openWorldHint=1` means the tool can communicate with Git remotes, GitHub,
Coolify, or another network destination. Annotations describe effects honestly and
do not replace server-side enforcement.

## Repository and local development

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `system_runtime_info` | 1/0/1/0 | Return safe live build and deterministic catalog identity. |
| `mcp_client_capabilities` | 1/0/1/0 | Return only the current session's allowlisted client name/version, protocol and explicitly announced sampling/roots/elicitation flags. |
| `model_runtime_start` | 0/0/0/0 | Create one durable external-model runtime; it does not start or select a model provider. |
| `opencode_runtime_start` | 0/0/1/0 | Historical compatibility name for the active signed Edge model harness; current signed candidates use Codex while a bundle rollback may restore OpenCode. |
| `codex_runtime_start` | 0/0/1/0 | Request one pinned stock Codex runtime on an active Edge device using only opaque device/workspace identity, a bounded goal, timeout, and idempotency key. |
| `project_task_start` | 0/0/1/1 | Start or reuse one durable task group with one to four bounded goals. Each goal receives a separately leased and fenced server-owned Git worktree, registered Edge workspace, branch and independent stock Codex runtime; no worker shares a writer checkout. |
| `project_task_status` | 1/0/1/0 | Reconcile and return separate task lifecycle, model-runtime and semantic-acceptance states. A completed runtime becomes `acceptance_pending` only after bounded live Git evidence reports the exact base/head, cleanliness, commits ahead and changed-path count; missing or inconsistent evidence becomes `reconciliation_required`, never success. Prompts, leases, fences, paths, credentials and process internals remain private. |
| `project_task_cancel` | 0/1/1/0 | Idempotently request cancellation of every nonterminal worker runtime in one durable task group while retaining terminal evidence and worktree branches. |
| `project_task_cleanup` | 0/1/1/0 | Remove only terminal, clean, exact-fence managed worktrees for one task. Worker branches and durable task evidence remain available for explicit review and integration. |
| `workspace_runtime_continue` | 0/0/1/0 | Continue one registered dev or HTB workspace through the active ChatGPT session using its local trusted contract; accepts the opaque workspace id, timeout and a fresh caller-generated idempotency key, creates one runtime, and does not retry automatically. |
| `workspace_lab_prepare` | 0/0/1/0 | Queue idempotent HTB Linux workspace preparation on a paired Edge using closed lab metadata; commands and credentials never enter the control plane. |
| `project_prepare` | 0/0/1/1 | Create, recover, or associate one development project using only project alias, repository name and human Edge target alias; local Git authority, paths and opaque IDs remain inside the Edge. |
| `project_status` | 1/0/1/0 | Resolve one Edge project by alias and human target, returning only safe repository, profile, mode, readiness or blocker metadata. |
| `project_snapshot` | 1/0/1/0 | Queue or reuse one durable Edge operation by caller idempotency key, resolve the selected development workspace locally, run only fixed read-only Git identity/cleanliness commands, and return bounded repository, branch, commit and operation metadata without starting another model. |
| `project_exec` | 0/1/1/1 | Execute one durable bounded foreground argv inside the selected trusted development workcell through Bubblewrap, with workspace-only writable state, relative cwd, optional stdin and non-secret environment, process-group cancellation, a 120-second maximum timeout, separate bounded redacted stdout/stderr, and safe preflight/execution/result durations. No implicit shell is added. |
| `project_network_route` | 1/0/1/1 | Resolve the selected Edge workcell route to one private IPv4 destination and return only the validated `tun*`/`tap*` interface and source IPv4. No executable, URL, path or credential is accepted. |
| `project_network_probe` | 1/0/1/1 | Perform at most 64 explicit TCP connect probes to one private IPv4 destination after validating a `tun*`/`tap*` VPN route. Ports and a 50-1500 ms per-port timeout are structured inputs; results use closed port states. |
| `project_browser_harness_start` | 0/1/1/1 | Start or reuse arbitrary argv in the project's persistent rootless toolbox for Playwright, Puppeteer, Selenium, WebDriver, browser CLIs or any language/framework installed by the project. Standard managed run, artifacts, downloads and persistent-profile directories are supplied through environment variables; no browser action, JavaScript, domain or browser-engine allowlist is imposed. |
| `project_browser_harness_status` | 1/0/1/0 | Read one durable harness run's opaque lifecycle, configurable timeout/storage limits, bounded incremental redacted stdout/stderr and aggregate artifact metadata without returning argv, environment, PID, cookies, host paths or profile contents. |
| `project_browser_harness_list` | 1/0/1/0 | List at most 50 durable harness runs bound to one authorized development project and Edge target. |
| `project_browser_harness_stop` | 0/1/1/0 | Idempotently stop one owned harness process tree after PID/start-time revalidation, bounded TERM grace and KILL only when required. |
| `project_browser_harness_cleanup` | 0/1/1/0 | Explicitly remove terminal managed run directories and metadata; persistent profiles are removed only on request and only when no retained run uses them. |
| `project_browser_harness_artifact_list` | 1/0/1/0 | List bounded metadata for arbitrary regular files under a run's managed `artifacts/` or `downloads/` trees, including screenshots, PDFs, traces, videos, HARs, logs and downloads. |
| `project_browser_harness_artifact_read` | 1/0/1/0 | Read one exact bounded base64 chunk from a relative managed artifact/download path; traversal, symlinks and non-owned files fail closed. |
| `project_browser_create` | 0/0/1/1 | Convenience API: create or reuse one durable Edge-private Chromium session using the authorized workcell's general HTTP/HTTPS network, including Internet, private endpoints and localhost. |
| `project_browser_status` | 1/0/1/0 | Convenience API: read one bounded Chromium-session summary with opaque identity, safe URL without query/fragment, title, state, revision and timestamps. |
| `project_browser_list` | 1/0/1/0 | Convenience API: list at most 20 Chromium-session summaries for one registered project and Edge target. |
| `project_browser_run` | 0/1/1/1 | Convenience API: execute up to 32 common navigation/locator actions in ephemeral Chromium backed by a persistent Edge-private profile. It is not the only automation path; arbitrary code uses `project_browser_harness_start`. Downloads are allowed into the managed profile. |
| `project_browser_artifact_read` | 1/0/1/0 | Convenience API: read one bounded base64 chunk from an Edge-private JPEG capture by opaque session/artifact identities and byte offset. |
| `project_browser_close` | 0/1/1/0 | Convenience API: idempotently close one non-busy Chromium session while preserving its private profile and artifacts until cleanup. |
| `project_browser_cleanup` | 0/1/1/0 | Convenience API: explicitly remove only closed Chromium sessions, exact private profiles and exact JPEG artifacts. Ready/busy sessions are preserved. |
| `project_process_start` | 0/1/1/1 | Start or reuse one durable background argv through the same Bubblewrap/workcell executor as `project_exec`, keyed by a caller idempotency key. It returns an opaque process id; PID, paths, argv and environment remain private. |
| `project_process_status` | 1/0/1/0 | Read safe durable state plus bounded incremental redacted stdout/stderr for one owned background process by opaque id and byte offsets. |
| `project_process_stdin` | 0/1/1/1 | Write one ordered bounded non-secret UTF-8 chunk to an owned durable process stdin, or close stdin explicitly, using an idempotency key and expected byte offset. The incremental stream is capped at 16 MiB; receipts report exact accepted bytes and closed state without returning input content. |
| `project_process_stop` | 0/1/1/0 | Idempotently stop one owned process group after PID/start-time revalidation, using TERM followed by bounded grace and KILL only when needed. |
| `project_process_signal` | 0/1/0/0 | Send one closed `interrupt`, `terminate`, or `kill` signal to an owned process group after PID/start-time revalidation; arbitrary signals and host-wide targets are rejected. |
| `project_process_list` | 1/0/1/0 | List at most 100 opaque process identities, states and timestamps for one project/target without exposing PID, argv, environment, paths or log contents. |
| `project_process_cleanup` | 0/1/1/0 | Explicitly remove terminal journal records and private logs for one process or a project/target; live processes are reported and preserved. |
| `project_git_status` | 1/0/1/1 | Inspect the registered Edge checkout and fixed owner-bound `origin`, returning only branch/commit/remote relation and clean, detached, unpublished or diverged state. No path, URL or credential is returned. |
| `project_github_status` | 1/0/1/1 | Ask the local Edge broker to verify the configured GitHub authority against the repository already bound to a development project. The broker invokes only fixed `gh api` reads and returns bounded repository metadata plus closed permission diagnostics; token, URL, headers and CLI output never enter the public result. |
| `project_git_fetch` | 0/0/1/1 | Fetch exactly `origin` with `--no-tags` and an Edge-constructed current-branch refspec, using the existing private Git broker credential; no caller refspec is accepted. |
| `project_git_fast_forward_preview` | 1/0/1/1 | Create a five-minute Edge-owned single-use plan bound to project, target, branch, clean tree, local HEAD and fetched remote HEAD. Dirty, detached, ahead or diverged checkouts fail closed. |
| `project_git_fast_forward` | 0/0/1/1 | Consume and revalidate one exact plan, then run only `git merge --ff-only <bound-commit>`; no reset, checkout, force, tags, URL or free refspec exists. |
| `project_git_publish_preview` | 1/0/1/1 | Create a five-minute Edge-owned single-use publication plan bound to the exact clean attached branch, local HEAD and current same-name remote-branch state. An existing remote commit must be locally resolvable and a proven ancestor; a stale optional tracking ref does not replace that proof. |
| `project_git_publish` | 0/0/1/1 | Consume and revalidate one exact publication plan, then push only the bound branch to its same-name branch on the fixed owner-bound `origin` with no force, tags, caller URL or caller refspec. |
| `project_toolbox_create` | 0/1/1/1 | Create or recover one persistent Debian rootfs through the registered workspace's user-owned rootless engine. Optional CPU, memory and process limits are range-checked; omitted values receive broad server-owned defaults. The fixed rootless endpoint is available inside the toolbox; packages and caches persist until explicit cleanup. |
| `project_toolbox_status` | 1/0/1/1 | Return only opaque lifecycle/base identity, applied resource limits, rootless-engine availability, writable/rootfs byte usage and timestamps; host paths, socket path and container identity remain private. |
| `project_toolbox_repair` | 0/1/1/1 | Restart only a stopped server-owned toolbox after revalidating its recorded image, labels and single workspace mount. Missing, unknown or unsafe state is never recreated. |
| `project_toolbox_exec` | 0/1/1/1 | Execute explicit arbitrary argv inside the persistent toolbox with the project at `/workspace`, relative cwd, non-secret environment overlay, bounded redacted output and no implicit shell or command allowlist. Installed Podman/Docker/Compose clients use the fixed user-owned rootless endpoint. |
| `project_toolbox_install` | 0/1/1/1 | Run explicit package, toolchain or rootless container-client installation argv as container root inside the rootless user namespace; the host WSL package database and global toolchains are not modified. |
| `project_toolbox_service_start` | 0/1/1/1 | Start or reuse one named background argv inside the persistent toolbox. Only an opaque service id, name, state and timestamps are returned; caller argv is positional and never interpolated into the fixed supervisor script. |
| `project_toolbox_service_status` | 1/0/1/1 | Revalidate one opaque service identity and report `running` or `stopped` without starting a stopped toolbox or exposing PID, argv, paths, logs or container internals. |
| `project_toolbox_service_stop` | 0/1/1/1 | Stop one owned service after PID/start-tick revalidation using TERM, a bounded grace period and KILL only when necessary; repeated requests are idempotent. |
| `project_toolbox_cleanup` | 0/1/1/1 | Explicitly remove only the project's toolbox rootfs and its private metadata. Cleanup is idempotent, never automatic, and does not delete the project workspace. |
| `edge_operation_list` | 1/0/1/0 | List bounded queued/running operation identity, kind, progress and cancellation state for one human Edge target without exposing device/workspace ids, paths, request bodies or raw output. |
| `edge_operation_status` | 1/0/1/0 | Read one durable Edge operation's bounded lifecycle, progress and derived queue/pickup/work/completion/total durations by operation id; internal absolute phase timestamps remain private. |
| `edge_operation_cancel` | 0/1/1/0 | Idempotently cancel one queued operation or one interruptible running operation; updater, rollback and repair effects become non-cancellable after pickup. |
| `workspace_lab_retarget` | 0/0/1/0 | Queue a private-IP retarget; the Edge validates VPN routing and rotates local authorization while preserving the workspace ID and evidence. |
| `workspace_autopilot_start` | 0/0/1/0 | Start or reuse one durable local job with `run_until=completed_or_cancelled`; no free-form objective is accepted. |
| `workspace_autopilot_status` | 1/0/1/0 | Return signed, content-free job state, progress revision, cycle count and safe blocker code. |
| `workspace_autopilot_pause` | 0/0/1/0 | Pause the local job after its current bounded cycle without discarding checkpoint or evidence. |
| `workspace_autopilot_resume` | 0/0/1/0 | Resume a paused or safely blocked job using the existing local state and provider configuration. |
| `workspace_autopilot_cancel` | 0/1/1/0 | Cancel the durable job and prevent further local cycles while preserving collected evidence. |
| `edge_bundle_status` | 1/0/1/0 | Return only signed release, commit, manifest/component compatibility, service health, known systemd restart count and update availability metadata from one paired Edge. |
| `edge_bundle_update` | 0/0/1/0 | Request only `release=stable`; the restricted root updater resolves and verifies the official signed channel. |
| `edge_bundle_rollback` | 0/1/1/0 | Activate only the previous locally known signed release and verify Edge health. |
| `edge_repair` | 0/0/1/0 | Restore only reviewed signed components, permissions, fixed symlinks, packaged unit and Edge health. |
| `edge_onboarding_status` | 1/0/1/0 | Return safe pairing, service, known systemd restart count, bundle, provider, driver, Bubblewrap, rootless, workspace count and blocker metadata. |
| `model_runtime_status` | 1/0/1/0 | Return only public runtime identity, state, controller, sequence, update time, optional result ref, and the bounded server-owned startup phase timeline. |
| `model_turn_next` | 1/0/1/0 | Poll for the next awaiting turn and return its canonical request plus offered tool ids. |
| `model_turn_respond` | 0/0/0/0 | Submit one bounded text/tool-call response after runtime, sequence, digest and offered-tool validation. |
| `model_runtime_cancel` | 0/1/1/0 | Idempotently cancel a runtime and all active unconsumed turns. |
| `build_context_pack` | 1/0/1/0 | Read a compact jailed repo context pack. |
| `workspace_checkpoint` | 1/0/1/0 | Return a bounded schema-only Git/task checkpoint without fetch, file bodies, absolute paths, or external calls. |
| `list_dir` | 1/0/1/0 | Compatibility name for `repo_list`. |
| `repo_list` | 1/0/1/0 | List jailed directories and identify Git repos. |
| `read_file` | 1/0/1/0 | Read one file with secret-path grants and redaction. |
| `read_many_files` | 1/0/1/0 | Read several independently policy-checked files. |
| `search_code` | 1/0/1/0 | Search jailed source with redacted results. |
| `result_read` | 1/0/1/0 | Read a redacted persisted result by opaque ref in fragments capped at 16 KiB. |
| `result_find` | 1/0/1/0 | Find unexpired redacted results by exact bounded substring; metadata only. |
| `result_stage` | 1/0/1/0 | Read one indexed result stage by opaque ref in fragments capped at 16 KiB. |
| `apply_patch` | 0/1/0/0 | Validate and apply a diff that may replace or delete content. |
| `create_file` | 0/0/0/0 | Create a new file through the patch pipeline; no overwrite. |
| `run_command` | 0/1/0/1 | Run one allowlisted argv without a shell; may reach network. |
| `run_tests` | 0/1/0/1 | Run the configured allowlisted test command; may reach network. |
| `project_validation_preview` | 1/0/1/0 | Preview one fixed `pnpm-lockfile` or `pnpm-validate` private-runner profile. |
| `project_validation_execute` | 0/1/0/1 | Execute one reviewed fixed Node/pnpm profile through the private runner. |
| `git_status` | 1/0/1/0 | Compatibility name for `repo_status`. |
| `repo_status` | 1/0/1/0 | Return branch, HEAD, upstream, ahead/behind and file state. |
| `git_diff` | 1/0/1/0 | Compatibility name for `repo_diff`. |
| `repo_diff` | 1/0/1/0 | Return a jailed read-only Git diff. |
| `git_clone` | 0/0/0/1 | Clone a credential-free URL into a new jailed directory. |
| `repo_fetch` | 0/0/1/1 | Run exactly `git fetch <remote>`; approval-gated in ask mode. |
| `repo_fast_forward_preview` | 1/0/1/0 | Plan an exact clean-tree fast-forward to the tracked upstream. |
| `repo_fast_forward` | 0/0/0/0 | Revalidate and run exactly `git merge --ff-only <upstream>`. |
| `git_commit` | 0/0/0/0 | Stage and commit locally. It does not push. |

## GitHub source hosting and publication

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `github_repo_info` | 1/0/1/1 | Compatibility name for `source_repo_info`. |
| `source_repo_info` | 1/0/1/1 | Read owner-bound repository metadata and permission. |
| `source_repo_create_preview` | 1/0/1/1 | Confirm absence and plan private-by-default creation. |
| `github_create_repo` | 0/0/0/1 | Compatibility name for planned `source_repo_create`. |
| `source_repo_create` | 0/0/0/1 | Revalidate and create the planned owner-bound repository. |
| `source_public_issue_status` | 1/0/1/1 | Read one public upstream issue, assignees, bounded conversation and linked PRs. |
| `source_public_fork_create_preview` | 1/0/1/1 | Verify one public external repository and plan a fork under the configured owner. |
| `source_public_fork_create` | 0/0/0/1 | Revalidate and create the planned public fork, then verify its parent and write permission. |
| `source_public_issue_comment_preview` | 1/0/1/1 | Freeze one open public issue/PR conversation and plan an exact comment. |
| `source_public_issue_comment` | 0/0/0/1 | Require the conversation to remain unchanged and post the planned comment. |
| `source_public_review_reply_preview` | 1/0/1/1 | Bind one exact inline review comment, PR head and reply body. |
| `source_public_review_reply` | 0/0/0/1 | Revalidate the PR and comment timestamp, then post one threaded reply. |
| `source_cross_repo_pull_request_create_preview` | 1/0/1/1 | Bind fork/upstream SHAs, ancestry and duplicate state for one public PR. |
| `source_cross_repo_pull_request_create` | 0/0/0/1 | Revalidate the fork, SHAs and duplicate state, then open the public upstream PR. |
| `source_public_pull_request_status` | 1/0/1/1 | Read one public PR, exact-head checks, reviews and conversation comments. |
| `source_pull_request_create_preview` | 1/0/1/1 | Bind head/base SHAs and plan one non-draft pull request. |
| `source_pull_request_create` | 0/0/0/1 | Revalidate branch SHAs and create the planned pull request. |
| `source_pull_request_status` | 1/0/1/1 | Read PR state and every check/status context for the exact head SHA. A generic required-checks denial keeps evidence incomplete; only GitHub's exact feature-unavailable response for an ineligible private repository proves that no required-check feature exists. |
| `source_pull_request_failure_diagnostics` | 1/0/1/1 | Read failed jobs on the exact PR head and return failed steps, annotations and line-numbered redacted log context. |
| `source_pull_request_job_log` | 1/0/1/1 | Read one exact job log in redacted byte chunks with `next_offset`; no job ID, token or signed URL is exposed. |
| `source_pull_request_merge_preview` | 1/0/1/1 | Require mergeable state and completely green checks, then plan a merge commit. |
| `source_pull_request_merge` | 0/1/0/1 | Revalidate head, mergeability and checks, then merge with `merge_method=merge`. |
| `source_default_branch_update_preview` | 1/0/1/1 | Bind the existing target branch SHA and plan a default-branch update. |
| `source_default_branch_update` | 0/1/0/1 | Revalidate the target SHA and update the owner-bound repository default branch. |
| `source_workflow_dispatch_preview` | 1/0/1/1 | Verify one active workflow file and exact branch SHA, reject secret-like bounded inputs, and plan one dispatch. |
| `source_workflow_dispatch` | 0/1/0/1 | Revalidate workflow identity and branch SHA, then dispatch the reviewed owner-bound workflow once. |
| `source_edge_release_status` | 1/0/1/1 | Read the maintainer profile's fixed `mcp-devbox` `edge-release` state, release runs/jobs, and release assets. |
| `source_edge_release_maintenance_preview` | 1/0/1/1 | Plan cancellation of obsolete release runs followed by the fixed main-only custom deployment branch policy. |
| `source_edge_release_maintenance_apply` | 0/1/0/1 | Revalidate and execute only that fixed maintenance plan; branch protection is never changed. |
| `repo_remote_preview` | 1/0/1/0 | Plan an owner-restricted credential-free remote add/update. |
| `repo_remote_set` | 0/1/0/0 | Revalidate and add or replace the planned named remote. |
| `repo_publish_preview` | 1/0/1/1 | Inspect the exact remote branch and plan one safe push. |
| `git_push` | 0/0/0/1 | Compatibility name for planned `repo_publish`. |
| `repo_publish` | 0/0/0/1 | Revalidate and push one branch; no force/tags/mirror/refspecs. |

The public catalog GitHub tools use the VPS/Coolify `GITHUB_TOKEN` for API operations
such as repository metadata, owner-bound publication, public fork creation, issue/PR
comments, cross-repository pull requests, exact-head checks, Actions diagnostics and
owner-bound merge. Public OSS operations accept only a public external upstream, create
forks under the configured owner, keep upstream read-only, use expiring single-use
plans for every write, and do not expose an external merge operation.
Actions runs/jobs/logs require `Actions: Read`; workflow dispatch requires
`Actions: Write`; check-run annotations require `Checks: Read`. Job-log downloads follow exactly one GitHub-issued redirect, omit the
Authorization header on the signed download request, redact returned content and expose
at most 1 MiB per call within a 16 MiB per-job read window. A configured local
development Edge separately exposes `workspace_dev_git_clone`,
`workspace_dev_publish_preview`, and `workspace_dev_publish` through its private signed
harness broker. Those owner-bound transport actions are intentionally absent from the
exterior MCP catalog; see `docs/development-edge-git.md`.

## Coolify platform

The fixed production backend, Front Door/coordinator, Brain deployment contract, and
official Edge-release maintenance operations require the explicit repository-maintainer
profile. They remain registered for catalog compatibility but fail closed in the
portable default configuration. Generic app status, creation, deployment, logs, and
domain operations continue to use only the operator's configured Coolify boundaries.

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `coolify_list_apps` | 1/0/1/1 | Compatibility name for `platform_apps_list`. |
| `platform_apps_list` | 1/0/1/1 | Return safe application summaries. |
| `coolify_app_status` | 1/0/1/1 | Compatibility name for `platform_app_status`. |
| `platform_app_status` | 1/0/1/1 | Return one allowed application's safe status. |
| `coolify_app_logs` | 1/0/1/1 | Compatibility name for bounded, redacted `platform_app_logs`. |
| `platform_app_logs` | 1/0/1/1 | Return bounded and redacted logs for one allowed application. |
| `coolify_deployment_status` | 1/0/1/1 | Compatibility name for one deployment's safe status summary. |
| `platform_deployment_status` | 1/0/1/1 | Return one deployment's status, commit, timestamps, application name, and only an unambiguous allowlisted Front Door coordinator `safe_code` when present in retained deployment logs. |
| `platform_app_create_preview` | 1/0/1/1 | Validate and plan owner/domain-restricted app creation. |
| `platform_app_domain_update_preview` | 1/0/1/1 | Validate one allowed healthy app, exact finished deployment (resolving owner-bound `HEAD` to its branch SHA), exact HTTPS origin and frozen non-secret configuration, then create a single-use domain-only plan. |
| `platform_app_domain_update` | 0/1/0/1 | Revalidate and PATCH only `domains` with conflict override disabled; preserve the existing deployment/configuration and compensate to the prior domain if verification detects drift. |
| `platform_front_door_create_preview` | 1/0/1/1 | Plan one fixed independently deployed MCP facade with allowed domain/backend and exact protocol/catalog pins. |
| `platform_front_door_create` | 0/1/0/1 | Create or reconcile only the managed facade, configure its authenticated non-secret compatibility variables, allow at most one exact transition catalog, and deploy only when the pinned commit and catalog state are already active. |
| `platform_front_door_status` | 1/0/1/1 | Read the managed facade by fixed server-owned name without exposing environment values or requiring its UUID in the general app allowlist. |
| `platform_front_door_coordinator_preview` | 1/0/1/1 | Plan the single private no-domain coordinator worker and its dedicated persistent journal. |
| `platform_front_door_coordinator_create` | 0/1/0/1 | Create or reconcile the private coordinator with fixed application identities and one normal deployment. |
| `platform_front_door_transition_preview` | 1/0/1/1 | Reconstruct the fixed topology and plan a cutover or rollback disposition as dispatch, observe, or noop. |
| `platform_front_door_transition` | 0/1/0/1 | Dispatch one reviewed closed target to the independent coordinator without changing domains in the backend request. |
| `platform_front_door_transition_status` | 1/0/1/1 | Read the coordinator contract, durable monotonic journal status, and current fixed topology. |
| `coolify_create_app` | 0/0/0/1 | Compatibility name for planned `platform_app_create`. |
| `platform_app_create` | 0/0/0/1 | Create one app from an unexpired single-use plan. |
| `platform_validation_runner_create_preview` | 1/0/1/1 | Plan one private validation runner using administrator-owned destination and mounts. |
| `platform_validation_runner_create` | 0/0/0/1 | Create that runner without deploying it or accepting secret values. |
| `platform_deploy_preview` | 1/0/1/1 | Plan deployment bound to app/repo/branch/commit state. |
| `coolify_deploy` | 0/1/0/1 | Compatibility name for planned `platform_deploy`. |
| `platform_deploy` | 0/1/0/1 | Revalidate application state and replace a live deployment. |
| `platform_deploy_without_cache_preview` | 1/0/1/1 | Plan a force=true deployment bound to app/repo/branch/commit state. |
| `platform_deploy_without_cache` | 0/1/0/1 | Revalidate and request a Coolify rebuild/deploy without reusable build cache. |
| `coolify_set_env` | 0/1/0/1 | Set/replace validated env keys; values are never returned or audited. |

## Brain memory

The five Brain tools are always present so the catalog remains deterministic. When
`MCP_DEVBOX_BRAIN_ROOT` is unset, each fails closed with the same
`brain is not configured` result. When set to a dedicated absolute root such as
`/brain`, startup initializes local Git, opens the disposable FTS5 cache, performs a
strict full reindex, and fails closed on any invalid source or permission. Retrieval is
explicitly on-demand; no complete Brain is injected into initialization or every
session. See `docs/runbooks/brain-operations.md`.

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `brain_search` | 1/0/1/0 | BM25 search over bounded quoted plain-text terms; at most 20 short redacted matches. |
| `brain_read` | 1/0/1/0 | Read one strict slug with trust metadata and at most 128 backlinks. |
| `brain_write` | 0/0/0/0 | Create/update only agent-owned `working/` Markdown with provenance and review date; curated/path/timestamps are not inputs. |
| `brain_index` | 0/0/1/0 | Return index status or transactionally rebuild only the disposable cache from Markdown truth. |
| `brain_context` | 1/0/1/0 | Return at most 16 one-line note summaries / 4 KiB, curated first, without note bodies. |

Brain schemas are closed (`additionalProperties=false`) and enforce slug, author,
type, date, query, result and content bounds. Secret-shaped agent writes are rejected;
manual source content is redacted before cache insertion and again before return.

## Memory, notes, sandbox, and privileged profiles

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `memory_read` | 1/0/1/0 | Read redacted structured project memory. |
| `memory_write` | 0/1/0/0 | Replace one closed structured memory section. |
| `memory_update_handoff` | 0/1/0/0 | Write a handoff and replace the `latest.md` pointer. |
| `notes_list` | 1/0/1/0 | List global Markdown note metadata. |
| `notes_read` | 1/0/1/0 | Read one jailed, non-symlink note with redaction. |
| `notes_write_preview` | 1/0/1/0 | Plan size-limited create/append without overwrite. |
| `notes_write` | 0/0/0/0 | Revalidate hash/state and create or append the note. |
| `sandbox_status` | 1/0/1/0 | Attest and report the private rootless L3 executor; unavailable on endpoint/image/profile drift. |
| `sandbox_exec` | 0/1/0/0 | Run arbitrary explicit argv in the private rootless sandbox; L1 allowlists do not apply. |
| `privileged_task_preview` | 1/0/1/0 | Preview one fixed administrator-enabled profile. |
| `privileged_task_execute` | 0/1/0/1 | Execute one exact short-lived profile plan with timeout. |

## Recommended repository workflow

Use one tool call per message when the ChatGPT connector is sensitive to multi-tool
message sequencing:

1. `repo_list`
2. `repo_status`
3. `repo_fetch`
4. `repo_fast_forward_preview`
5. `repo_fast_forward`
6. edit, verify, then `git_commit`
7. `source_repo_create_preview`
8. `source_repo_create`
9. `repo_remote_preview`
10. `repo_remote_set`
11. `repo_publish_preview`
12. `repo_publish`

Steps 7-10 are needed only when creating/configuring a new GitHub repository.
`git_commit` does not push. External writes require explicit approval in ask mode.

## Recommended public OSS contribution workflow

1. `source_public_issue_status`
2. repeat issue/comment/linked-PR review before claiming work
3. `source_public_fork_create_preview`, then `source_public_fork_create`
4. prepare the configured-owner fork locally, implement, verify and commit with DCO
5. publish one branch through `repo_publish_preview` / `repo_publish`
6. `source_public_issue_comment_preview`, then `source_public_issue_comment` when the project requires a claim
7. `source_cross_repo_pull_request_create_preview`, then `source_cross_repo_pull_request_create`
8. `source_public_pull_request_status` for checks, reviews, inline review comments and conversation
9. use `source_public_review_reply_preview` / `source_public_review_reply` for an exact inline thread

The broker never merges an external upstream PR. Maintainers retain merge authority.

## Recommended Coolify workflow

1. `platform_apps_list`
2. `platform_app_create_preview`
3. `platform_app_create`
4. `platform_deploy_preview`
5. `platform_deploy` or the explicit `platform_deploy_without_cache_preview` / `platform_deploy_without_cache` pair
6. `platform_app_status`
7. `platform_app_logs`

Required environment variables are supplied by the administrator, never through
repo content. Tokens are sent only in HTTP authorization headers and never returned.

## Administrator environment

- Core/transport: `MCP_DEVBOX_TOKEN`, `MCP_DEVBOX_ROOT`, `MCP_DEVBOX_MODE`,
  `MCP_DEVBOX_TEST_CMD`, `MCP_DEVBOX_ALLOW_CMD`, `MCP_DEVBOX_BRAIN_ROOT`,
  optional repository-only `MCP_DEVBOX_MAINTAINER_PROFILE`,
  `MCP_DEVBOX_PUBLIC_URL`,
  `MCP_DEVBOX_OAUTH_PASSPHRASE`, `MCP_DEVBOX_OAUTH_CLIENT_STORE`, and
  `MCP_DEVBOX_OAUTH_ACCESS_STORE` and `MCP_DEVBOX_OAUTH_REFRESH_STORE` as applicable.
- GitHub: `GITHUB_TOKEN`, `GITHUB_OWNER`, `GITHUB_OWNER_TYPE` (`user` or `org`),
  and optional `GITHUB_DEFAULT_VISIBILITY` (`private` by default).
- Coolify: `COOLIFY_URL`, `COOLIFY_API_TOKEN`, `COOLIFY_SERVER_UUID`,
  `COOLIFY_PROJECT_UUID`, either `COOLIFY_ENVIRONMENT_UUID` or
  `COOLIFY_ENVIRONMENT_NAME`, and optional `COOLIFY_ALLOWED_APPS` /
  `COOLIFY_ALLOWED_DOMAINS`.
- Privileged profiles: `MCP_DEVBOX_PRIVILEGED_TASKS=true` to enable,
  optional `MCP_DEVBOX_PRIVILEGED_SERVICES`, and optional
  `MCP_DEVBOX_PRIVILEGED_TIMEOUT` (default `2m`).

These values are startup configuration. No MCP tool can mutate security policy or
return their secret values.

Private validation runner: `MCP_DEVBOX_VALIDATION_RUNNER_URL` (internal Docker
network only) and `MCP_DEVBOX_VALIDATION_RUNNER_TOKEN`. It exposes fixed pnpm
profiles only; see `docs/validation-runner.md`. Do not add Node, npm, pnpm or
Corepack to the public MCP command allowlist.

## Notes and privileged workflows

Use `notes_list`/`notes_read` and `notes_write_preview`/`notes_write` for free-form
global notes. Keep `memory_write` for `current-task`, `plan`, `decisions`, and
`reflections`. Notes are not automatically committed into child repositories.

Privileged profiles require `MCP_DEVBOX_PRIVILEGED_TASKS=true`, then
`privileged_task_preview` followed by `privileged_task_execute`. There is no free
host terminal. Docker profiles fail securely in the public MCP architecture rather
than receiving the Docker socket. Go test/vet/build/format profiles require an
available sandbox and fail securely when containment is unavailable.
