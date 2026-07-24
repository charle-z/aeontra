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
	document := strings.ToLower(read("workqueue-store.md"))
	for _, required := range []string{
		"one active control-plane writer",
		"redis",
		"fencing counter",
		"stale completion",
		"dependency_failed",
		"1024 jobs",
		"64 per workspace",
		"64 mib",
		"backup",
		"restorebackup",
		"contains no source",
		"grants no authority",
	} {
		if !strings.Contains(document, strings.ToLower(required)) {
			t.Errorf("workqueue documentation missing %q", required)
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
