package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestDocumentationMapProtectsSourcesAndStatusVocabulary(t *testing.T) {
	content, err := os.ReadFile("documentation-map.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		".specify/memory/constitution.md",
		".agent-memory/current-task.md",
		".agent-memory/handoffs/latest.md",
		"specs/001-layer-1/",
		"docs/context-capsule.md",
		"docs/product-roadmap.md",
		"docs/runbooks/",
		"Deployed",
		"In progress",
		"Planned",
		"Not started",
		"Validation pending",
		"setup, configuration, permissions, validation, update, rollback, and troubleshooting",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("documentation map does not contain %q", required)
		}
	}
}
