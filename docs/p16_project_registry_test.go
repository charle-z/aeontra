package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP16ProjectAliasRegistryContractIsDocumented(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}
	document := read("project-workspace-resolution.md")
	for _, required := range []string{
		"projects.db",
		"There is intentionally no user-facing",
		"mcp-edge project discover --alias ekoparty",
		"mcp-edge project status --alias ekoparty",
		"reject traversal, underscores, Unicode and visually confusable characters",
		"fetch and push remotes are owner-bound HTTPS GitHub URLs",
		"workspace_conflict",
		"ambiguous_checkout",
		"discovery_timeout",
		"30-second total deadline",
		"approval_required",
		"compensates a newly",
		"checkout_dirty",
		"repository_mismatch",
		"project_prepare(alias=ekoparty",
		"project_status(alias=ekoparty",
		"There is intentionally no user-facing plan ID",
		"holds an open directory descriptor",
		"git clone --single-branch -- URL .",
		"credential_unavailable",
		"clone_failed",
		"cleanup_required",
		"associate one unique safe legacy path",
	} {
		if !containsNormalizedProse(document, required) {
			t.Errorf("project resolution documentation missing %q", required)
		}
	}
	for _, literal := range []string{
		"projects.db",
		"mcp-edge project discover --alias ekoparty",
		"mcp-edge project status --alias ekoparty",
		"workspace_conflict",
		"ambiguous_checkout",
		"discovery_timeout",
		"approval_required",
		"checkout_dirty",
		"repository_mismatch",
		"project_prepare(alias=ekoparty",
		"project_status(alias=ekoparty",
		"git clone --single-branch -- URL .",
		"credential_unavailable",
		"clone_failed",
		"cleanup_required",
	} {
		if !strings.Contains(document, literal) {
			t.Errorf("project resolution documentation missing literal %q", literal)
		}
	}
	mapDocument := read("documentation-map.md")
	if !strings.Contains(mapDocument, "docs/project-workspace-resolution.md") {
		t.Fatal("documentation map does not reference the P16 project registry contract")
	}
	tasks := read("../specs/007-global-work-scheduler/tasks.md")
	for _, required := range []string{
		"Resolve one registered project using alias only",
		"Add private versioned `projects.db` schema",
		"Add read-only project discover/resolve/status tools first",
		"Add bounded unbound local workspace discovery/classification",
		"Clone missing repo into approved root with fixed Git authority",
		"Expose approved create/associate tools without internal IDs",
	} {
		if !containsNormalizedProse(tasks, required) {
			t.Errorf("P16 tasks missing project registry checkpoint %q", required)
		}
	}
}
