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
| `opencode_runtime_start` | 0/0/1/0 | Request one pinned OpenCode runtime on an active Edge device using only opaque device/workspace identity, a bounded goal, timeout, and idempotency key. |
| `workspace_runtime_continue` | 0/0/1/0 | Continue one registered dev or HTB workspace through the active ChatGPT session using its local trusted contract; accepts the opaque workspace id, timeout and a fresh caller-generated idempotency key, creates one runtime, and does not retry automatically. |
| `workspace_lab_prepare` | 0/0/1/0 | Queue idempotent HTB Linux workspace preparation on a paired Edge using closed lab metadata; commands and credentials never enter the control plane. |
| `workspace_lab_retarget` | 0/0/1/0 | Queue a private-IP retarget; the Edge validates VPN routing and rotates local authorization while preserving the workspace ID and evidence. |
| `workspace_autopilot_start` | 0/0/1/0 | Start or reuse one durable local job with `run_until=completed_or_cancelled`; no free-form objective is accepted. |
| `workspace_autopilot_status` | 1/0/1/0 | Return signed, content-free job state, progress revision, cycle count and safe blocker code. |
| `workspace_autopilot_pause` | 0/0/1/0 | Pause the local job after its current bounded cycle without discarding checkpoint or evidence. |
| `workspace_autopilot_resume` | 0/0/1/0 | Resume a paused or safely blocked job using the existing local state and provider configuration. |
| `workspace_autopilot_cancel` | 0/1/1/0 | Cancel the durable job and prevent further local cycles while preserving collected evidence. |
| `edge_bundle_status` | 1/0/1/0 | Return only signed release, commit, manifest/component compatibility, service health and update availability metadata from one paired Edge. |
| `edge_bundle_update` | 0/0/1/0 | Request only `release=stable`; the restricted root updater resolves and verifies the official signed channel. |
| `edge_bundle_rollback` | 0/1/1/0 | Activate only the previous locally known signed release and verify Edge health. |
| `edge_repair` | 0/0/1/0 | Restore only reviewed signed components, permissions, fixed symlinks, packaged unit and Edge health. |
| `edge_onboarding_status` | 1/0/1/0 | Return safe pairing, service, bundle, provider, driver, Bubblewrap, rootless, workspace count and blocker metadata. |
| `model_runtime_status` | 1/0/1/0 | Return only public runtime identity, state, controller, sequence, update time, and optional result ref. |
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
| `source_pull_request_create_preview` | 1/0/1/1 | Bind head/base SHAs and plan one non-draft pull request. |
| `source_pull_request_create` | 0/0/0/1 | Revalidate branch SHAs and create the planned pull request. |
| `source_pull_request_status` | 1/0/1/1 | Read PR state and every check/status context for the exact head SHA. |
| `source_pull_request_merge_preview` | 1/0/1/1 | Require mergeable state and completely green checks, then plan a merge commit. |
| `source_pull_request_merge` | 0/1/0/1 | Revalidate head, mergeability and checks, then merge with `merge_method=merge`. |
| `source_default_branch_update_preview` | 1/0/1/1 | Bind the existing target branch SHA and plan a default-branch update. |
| `source_default_branch_update` | 0/1/0/1 | Revalidate the target SHA and update the owner-bound repository default branch. |
| `repo_remote_preview` | 1/0/1/0 | Plan an owner-restricted credential-free remote add/update. |
| `repo_remote_set` | 0/1/0/0 | Revalidate and add or replace the planned named remote. |
| `repo_publish_preview` | 1/0/1/1 | Inspect the exact remote branch and plan one safe push. |
| `git_push` | 0/0/0/1 | Compatibility name for planned `repo_publish`. |
| `repo_publish` | 0/0/0/1 | Revalidate and push one branch; no force/tags/mirror/refspecs. |

## Coolify platform

| Tool | R/D/I/O | Effect |
|---|---:|---|
| `coolify_list_apps` | 1/0/1/1 | Compatibility name for `platform_apps_list`. |
| `platform_apps_list` | 1/0/1/1 | Return safe application summaries. |
| `coolify_app_status` | 1/0/1/1 | Compatibility name for `platform_app_status`. |
| `platform_app_status` | 1/0/1/1 | Return one allowed application's safe status. |
| `coolify_app_logs` | 1/0/1/1 | Compatibility name for bounded, redacted `platform_app_logs`. |
| `platform_app_logs` | 1/0/1/1 | Return bounded and redacted logs for one allowed application. |
| `coolify_deployment_status` | 1/0/1/1 | Compatibility name for one deployment's safe status summary. |
| `platform_deployment_status` | 1/0/1/1 | Return one deployment's status, commit, timestamps, and application name. |
| `platform_app_create_preview` | 1/0/1/1 | Validate and plan owner/domain-restricted app creation. |
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
| `sandbox_status` | 1/0/1/0 | Report configured L3 containment status. |
| `sandbox_exec` | 0/1/0/0 | Run argv only inside an available L3 sandbox. |
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
  `MCP_DEVBOX_PUBLIC_URL`,
  `MCP_DEVBOX_OAUTH_PASSPHRASE`, `MCP_DEVBOX_OAUTH_CLIENT_STORE`, and
  `MCP_DEVBOX_OAUTH_REFRESH_STORE` as applicable.
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
