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
		"does not yet",
		"associate one unique safe legacy path",
		"clone a missing repository",
	} {
		if !strings.Contains(strings.ToLower(document), strings.ToLower(required)) {
			t.Errorf("project resolution documentation missing %q", required)
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
	} {
		if !strings.Contains(tasks, required) {
			t.Errorf("P16 tasks missing project registry checkpoint %q", required)
		}
	}
}
