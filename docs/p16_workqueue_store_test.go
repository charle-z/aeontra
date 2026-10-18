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
