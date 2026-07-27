package docs_test

import (
	"os"
	"strings"
	"testing"
)

func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestConnectRemoteDocumentsCurrentToolSurface(t *testing.T) {
	doc := readDoc(t, "connect-remote.md")

	for _, want := range []string{
		"configuration.md",
		"tools.md",
		"OAuth",
		"header-only recovery",
		"query-string credentials",
		"/healthz",
		"/version",
		"system_runtime_info",
		"workspace_checkpoint",
		"build_context_pack",
		"apply_patch",
		"preview",
		"single-use plan",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("connect-remote.md does not document %q", want)
		}
	}
}

func TestToolReferenceDocumentsAllRegisteredToolsAndInvariants(t *testing.T) {
	doc := readDoc(t, "tools.md")
	tools := []string{
		"apply_patch", "build_context_pack", "coolify_app_status", "coolify_create_app",
		"coolify_app_logs", "coolify_deploy", "coolify_list_apps", "coolify_set_env", "create_file",
		"git_clone", "git_commit", "git_diff", "git_push", "git_status",
		"github_create_repo", "github_repo_info", "list_dir", "memory_read",
		"memory_update_handoff", "memory_write", "notes_list", "notes_read",
		"notes_write", "notes_write_preview", "platform_app_create",
		"platform_app_create_preview", "platform_app_logs", "platform_app_status", "platform_apps_list",
		"platform_validation_runner_create_preview", "platform_validation_runner_create",
		"platform_deploy", "platform_deploy_preview", "privileged_task_execute",
		"privileged_task_preview", "read_file", "read_many_files", "repo_diff",
		"repo_fast_forward", "repo_fast_forward_preview", "repo_fetch", "repo_list",
		"repo_publish", "repo_publish_preview", "repo_remote_preview", "repo_remote_set",
		"repo_status", "run_command", "run_tests", "sandbox_exec", "sandbox_status",
		"search_code", "source_repo_create", "source_repo_create_preview", "source_repo_info",
		"project_validation_preview", "project_validation_execute",
		"brain_search", "brain_read", "brain_write", "brain_index", "brain_context",
	}
	if len(tools) != 62 {
		t.Fatalf("test inventory has %d tools, want 62", len(tools))
	}
	for _, name := range tools {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("tools.md does not document %s", name)
		}
	}
	for _, invariant := range []string{
		"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint",
		"git_commit does not push", "no force", "no free host terminal",
		"Tokens", "External writes require explicit approval", "aliases",
	} {
		if !strings.Contains(strings.ToLower(doc), strings.ToLower(invariant)) {
			t.Errorf("tools.md does not document invariant %q", invariant)
		}
	}
}

func TestFeaturesMarksWorkerPlanSuperseded(t *testing.T) {
	doc := strings.ToLower(readDoc(t, "features.md"))
	for _, want := range []string{
		"historical",
		"superseded",
		"cheap-model worker",
		"configuration.md",
		"security.md",
		"tools.md",
	} {
		if !strings.Contains(doc, strings.ToLower(want)) {
			t.Fatalf("features.md does not contain %q", want)
		}
	}
}

func TestL3SandboxPlanDocumentsHardRequirements(t *testing.T) {
	doc := readDoc(t, "l3-sandbox-plan.md")
	for _, want := range []string{
		"default-deny egress",
		"no Docker socket in the public MCP container",
		"169.254.169.254",
		"RFC1918",
		"explicit runner contract",
		"no free terminal before L3",
		"human approval",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("l3-sandbox-plan.md does not contain %q", want)
		}
	}
}
