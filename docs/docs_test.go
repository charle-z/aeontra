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
		"build_context_pack",
		"read_file",
		"read_many_files",
		"search_code",
		"apply_patch",
		"create_file",
		"run_command",
		"git_status",
		"git_diff",
		"run_tests",
		"git_commit",
		"memory_read",
		"memory_write",
		"memory_update_handoff",
		"MCP_DEVBOX_TEST_CMD",
		"MCP_DEVBOX_ALLOW_CMD",
		"one-tool-per-message",
		"git_commit does not push",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("connect-remote.md does not document %q", want)
		}
	}
}

func TestFeaturesMarksWorkerPlanSuperseded(t *testing.T) {
	doc := readDoc(t, "features.md")
	for _, want := range []string{
		"SUPERSEDED",
		"cheap-model worker",
		"docs/context-capsule.md",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("features.md does not contain %q", want)
		}
	}
}
