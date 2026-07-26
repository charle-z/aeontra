package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP16DurableEdgeJobJournalContractIsDocumented(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}
	document := read("edge-job-journal.md")
	for _, required := range []string{
		"Schema version `2`",
		"Persist `started` before calling the executor",
		"Persist `completed`",
		"jr_<sha256>",
		"does not call the executor again",
		"previous execution was interrupted; manual reconciliation required",
		"offline grace: 10m",
		"lease: 10m",
		"lease safety margin",
		"stops before the server may reassign",
		"before requesting any new lease",
		"offline grace exceeded; manual reconciliation required",
		"no new stage starts",
		"There is no silent VPS fallback",
		"never silently deleted",
		"seven days",
		"entry limit",
		"mcp-edge doctor",
		"migration_required",
		"read-only",
		"close/reopen persistence",
	} {
		if !containsNormalizedProse(document, required) {
			t.Errorf("durable Edge journal documentation missing %q", required)
		}
	}
	for _, literal := range []string{
		"`2`",
		"`started`",
		"`completed`",
		"jr_<sha256>",
		"offline grace: 10m",
		"lease: 10m",
		"mcp-edge doctor",
		"migration_required",
		"read-only",
	} {
		if !strings.Contains(document, literal) {
			t.Errorf("durable Edge journal documentation missing literal %q", literal)
		}
	}
	mapDocument := read("documentation-map.md")
	if !strings.Contains(mapDocument, "docs/edge-job-journal.md") {
		t.Fatal("documentation map does not reference durable Edge journal contract")
	}
	tasks := read("../specs/007-global-work-scheduler/tasks.md")
	for _, required := range []string{
		"[x] Persist `started` before executor invocation.",
		"[x] Network disconnect during bounded stage completes locally and reconnects.",
		"[x] Add local journal schema/state machine.",
		"[x] Add local doctor checks for journal integrity.",
	} {
		if !strings.Contains(tasks, required) {
			t.Errorf("P16 Step 4 tasks missing completion %q", required)
		}
	}
	service := read("../packaging/systemd/mcp-devbox-edge.service")
	if !strings.Contains(service, "--lease 10m") || strings.Contains(service, "--lease 1m") {
		t.Fatal("Edge systemd service must use the ten-minute lease authority")
	}
	main := read("../cmd/mcp-edge/main.go")
	if !strings.Contains(main, `fs.Duration("lease", 10*time.Minute`) {
		t.Fatal("mcp-edge run default lease must remain ten minutes")
	}
}
