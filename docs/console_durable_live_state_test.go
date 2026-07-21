package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConsoleDoc(t *testing.T, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "docs", relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestConsoleDurableLiveStateDocumentationContract(t *testing.T) {
	console := readConsoleDoc(t, "console.md")
	baseline := readConsoleDoc(t, filepath.Join("baselines", "2026-07-17-console-durable-live-state.md"))
	for _, required := range []string{
		"/state/tasks/tasks.db",
		"/state/console/sessions.db",
		"/state/brain/console-node.key",
		"console_metadata",
		"console_summary",
		"/console/event-log",
		"Last-Event-ID",
		"last_event_id",
		"20,000",
		"256 MiB",
		"presentation-only",
		"60-year practical persistence horizon",
		"sha256:c8f83d6aafeaba755fa601861564685a2f6167a9a73aac14034ecc51cd1ff941",
	} {
		if !strings.Contains(console, required) && !strings.Contains(baseline, required) {
			t.Fatalf("documentation missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Graph: real bounded Brain links with opaque ordinal IDs",
		"Sessions have an eight-hour expiry, are capped at 128 sessions, and are revoked on logout or process restart",
		"Events: safe browser refresh",
		"67 tools",
	} {
		if strings.Contains(console, forbidden) || strings.Contains(baseline, forbidden) {
			t.Fatalf("stale console contract retained %q", forbidden)
		}
	}
	if !strings.Contains(baseline, "not merged and not deployed") || !strings.Contains(baseline, "No merge or deployment") {
		t.Fatal("baseline must not claim deployment or authorize merge")
	}
}
