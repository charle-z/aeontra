package docs_test

import (
	"strings"
	"testing"
)

func TestAgentCatalogIntentIndexContract(t *testing.T) {
	doc := readDoc(t, "../AGENTS.md")

	for _, required := range []string{
		"## Tool Discovery Index",
		"| Intent | Canonical tool",
		"`workspace_checkpoint`",
		"`build_context_pack`",
		"`read_file` / `read_many_files`",
		"`search_code`",
		"`apply_patch`",
		"`create_file`",
		"`run_command` / `run_tests`",
		"`repo_status` / `repo_diff`",
		"`repo_publish_preview`",
		"`repo_publish`",
		"`source_pull_request_create_preview`",
		"`source_pull_request_create`",
		"`source_pull_request_status`",
		"`source_pull_request_failure_diagnostics`",
		"`source_pull_request_job_log`",
		"`source_pull_request_merge_preview`",
		"`source_pull_request_merge`",
		"`platform_apps_list`",
		"`platform_deploy_preview`",
		"`platform_deploy`",
		"`brain_context`",
		"`brain_read`",
		"`brain_search`",
		"`brain_write`",
		"`brain_index`",
		"`result_read` / `result_stage`",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("AGENTS.md catalog index missing %q", required)
		}
	}
}

func TestAgentCatalogRequiresCanonicalToolLookupBeforeHelpers(t *testing.T) {
	doc := readDoc(t, "../AGENTS.md")

	for _, required := range []string{
		"Before writing a script, HTTP client, Go program, or",
		"temporary helper, search the catalog",
		"search the catalog for a canonical tool",
		"create a helper when no existing tool covers the operation",
		"record briefly why",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("AGENTS.md helper rule missing %q", required)
		}
	}
}

func TestAgentCatalogDocumentsIntentSearchDecision(t *testing.T) {
	doc := readDoc(t, "../AGENTS.md")

	for _, required := range []string{
		"### Intent-search tool decision",
		"`tools/list`",
		"`api_tool.list_resources`",
		"No new catalog-search tool is justified yet",
		"bounded top-k",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("AGENTS.md intent-search decision missing %q", required)
		}
	}
}
