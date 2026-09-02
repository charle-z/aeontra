package catalog

import (
	"reflect"
	"testing"
)

type capturedAnnotation struct {
	Hints map[string]any
	Names []string
}

func TestRegisterAnnotationsDefinesStableSecurityPostures(t *testing.T) {
	var got []capturedAnnotation
	RegisterAnnotations(func(hints map[string]any, names ...string) {
		got = append(got, capturedAnnotation{Hints: hints, Names: names})
	})

	localRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	externalRead := map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false}
	localIdempotentWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}
	externalWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": true}
	externalIdempotentWrite := map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": true}
	localDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": false}
	externalDestructive := map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": false, "openWorldHint": true}

	want := []capturedAnnotation{
		{Hints: localRead, Names: []string{"system_runtime_info", "build_context_pack", "workspace_checkpoint", "list_dir", "repo_list", "read_file", "read_many_files", "search_code", "result_read", "result_find", "result_stage", "git_status", "repo_status", "git_diff", "repo_diff", "repo_fast_forward_preview", "repo_remote_preview", "privileged_task_preview", "project_validation_preview", "memory_read", "notes_list", "notes_read", "notes_write_preview", "sandbox_status", "brain_search", "brain_read", "brain_context"}},
		{Hints: externalRead, Names: []string{"github_repo_info", "source_repo_info", "source_repo_create_preview", "source_pull_request_create_preview", "source_pull_request_status", "source_public_issue_status", "source_public_fork_create_preview", "source_public_issue_comment_preview", "source_public_review_reply_preview", "source_cross_repo_pull_request_create_preview", "source_public_pull_request_status", "source_pull_request_failure_diagnostics", "source_pull_request_job_log", "source_pull_request_merge_preview", "source_default_branch_update_preview", "source_workflow_dispatch_preview", "repo_publish_preview", "coolify_list_apps", "platform_apps_list", "coolify_app_status", "platform_app_status", "coolify_app_logs", "platform_app_logs", "coolify_deployment_status", "platform_deployment_status", "platform_app_create_preview", "platform_app_domain_update_preview", "platform_validation_runner_create_preview", "platform_front_door_create_preview", "platform_front_door_status", "platform_deploy_preview", "platform_deploy_without_cache_preview"}},
		{Hints: localWrite, Names: []string{"create_file", "git_commit", "brain_write"}},
		{Hints: externalWrite, Names: []string{"git_clone", "git_push", "repo_publish", "github_create_repo", "source_repo_create", "source_pull_request_create", "source_public_fork_create", "source_public_issue_comment", "source_public_review_reply", "source_cross_repo_pull_request_create", "coolify_create_app", "platform_app_create", "platform_validation_runner_create"}},
		{Hints: externalIdempotentWrite, Names: []string{"repo_fetch"}},
		{Hints: localWrite, Names: []string{"repo_fast_forward"}},
		{Hints: localWrite, Names: []string{"notes_write"}},
		{Hints: localDestructive, Names: []string{"apply_patch", "memory_write", "memory_update_handoff", "repo_remote_set", "run_command", "run_tests", "sandbox_exec"}},
		{Hints: externalDestructive, Names: []string{"source_pull_request_merge", "source_default_branch_update", "source_workflow_dispatch", "coolify_deploy", "platform_deploy", "platform_deploy_without_cache", "platform_app_domain_update", "coolify_set_env", "platform_front_door_create", "privileged_task_execute", "project_validation_execute"}},
		{Hints: externalRead, Names: []string{"source_edge_release_status", "source_edge_release_maintenance_preview"}},
		{Hints: externalDestructive, Names: []string{"source_edge_release_maintenance_apply"}},
		{Hints: localIdempotentWrite, Names: []string{"brain_index"}},
	}
	want[1].Names = append(want[1].Names[:6], append([]string{"source_public_issue_create_preview"}, want[1].Names[6:]...)...)
	want[3].Names = append(want[3].Names[:7], append([]string{"source_public_issue_create"}, want[3].Names[7:]...)...)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("annotations = %#v, want %#v", got, want)
	}
}
