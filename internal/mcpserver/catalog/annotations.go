package catalog

// RegisterAnnotations emits truthful MCP behavior hints in the same order used by
// the server before catalog modularization.
func RegisterAnnotations(register func(map[string]any, ...string)) {
	localRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	externalRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	localIdempotentWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	externalWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": true}
	externalIdempotentWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false}
	externalDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": true}

	register(localRead,
		"system_runtime_info", "build_context_pack", "workspace_checkpoint", "list_dir", "repo_list", "read_file", "read_many_files",
		"search_code", "result_read", "result_find", "result_stage", "git_status", "repo_status", "git_diff", "repo_diff", "repo_fast_forward_preview", "repo_remote_preview", "privileged_task_preview", "project_validation_preview", "memory_read", "notes_list", "notes_read", "notes_write_preview", "sandbox_status",
		"brain_search", "brain_read", "brain_context")
	register(externalRead,
		"github_repo_info", "source_repo_info", "source_repo_create_preview", "repo_publish_preview", "coolify_list_apps", "platform_apps_list", "coolify_app_status", "platform_app_status", "coolify_app_logs", "platform_app_logs", "coolify_deployment_status", "platform_deployment_status", "platform_app_create_preview", "platform_validation_runner_create_preview", "platform_deploy_preview", "platform_deploy_without_cache_preview")
	register(localWrite, "create_file", "git_commit", "brain_write")
	register(externalWrite,
		"git_clone", "git_push", "repo_publish", "github_create_repo", "source_repo_create",
		"coolify_create_app", "platform_app_create", "platform_validation_runner_create")
	register(externalIdempotentWrite, "repo_fetch")
	register(localWrite, "repo_fast_forward")
	register(localWrite, "notes_write")
	register(localDestructive, "apply_patch", "memory_write", "memory_update_handoff", "repo_remote_set", "sandbox_exec")
	register(externalDestructive, "run_command", "run_tests", "coolify_deploy", "platform_deploy", "platform_deploy_without_cache", "coolify_set_env", "privileged_task_execute", "project_validation_execute")
	register(localIdempotentWrite, "brain_index")
}
