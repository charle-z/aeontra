package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP16WorkqueueStoreContractIsDocumented(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(body)
	}
	document := read("workqueue-store.md")
	for _, required := range []string{
		"one active control-plane writer",
		"fencing counter",
		"stale completion",
		"contains no source",
		"grants no authority",
		"schema version 2",
		"project_task_start",
		"managed worktree",
		"strictly newer fence",
		"preserves its Git branch",
	} {
		if !containsNormalizedProse(document, required) {
			t.Errorf("workqueue documentation missing %q", required)
		}
	}
	for _, literal := range []string{
		"Redis",
		"dependency_failed",
		"1024 jobs",
		"64 per workspace",
		"64 MiB",
		"Backup",
		"RestoreBackup",
	} {
		if !strings.Contains(document, literal) {
			t.Errorf("workqueue documentation missing literal %q", literal)
		}
	}
	mapDocument := read("documentation-map.md")
	if !strings.Contains(mapDocument, "docs/workqueue-store.md") {
		t.Fatal("documentation map does not reference workqueue store")
	}
	tasks := read("../specs/007-global-work-scheduler/tasks.md")
	for _, required := range []string{
		"[x] Validate all legal and illegal job transitions.",
		"[x] Add `internal/workqueue` store and migrations.",
		"[x] Add backup/restore fixture and operational docs.",
	} {
		if !strings.Contains(tasks, required) {
			t.Errorf("Step 5 checklist missing %q", required)
		}
	}
}

func TestManagedTaskWorktreeContractIsDocumented(t *testing.T) {
	workspace, err := os.ReadFile("project-workspace-resolution.md")
	if err != nil {
		t.Fatal(err)
	}
	security, err := os.ReadFile("security.md")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := os.ReadFile("tools.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		".mcp-devbox-worktrees",
		"codex/worktree-<id>",
		"clean tree",
		"no worker shares a writer checkout",
	} {
		combined := string(workspace) + "\n" + string(security) + "\n" + string(tools)
		if !strings.Contains(combined, required) {
			t.Errorf("managed worktree documentation missing %q", required)
		}
	}
	for _, tool := range []string{"project_task_start", "project_task_status", "project_task_cancel", "project_task_cleanup"} {
		if !strings.Contains(string(tools), "`"+tool+"`") {
			t.Errorf("task tool documentation missing %q", tool)
		}
	}
	workqueue, err := os.ReadFile("workqueue-store.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"acceptance_pending", "reconciliation_required", "runtime reaching `completed`", "does not prove"} {
		if !strings.Contains(string(workqueue), required) {
			t.Errorf("workqueue semantic status documentation missing %q", required)
		}
	}
}
