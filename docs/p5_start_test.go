package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5DeeperTestingDefinitionRemainsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/002-deeper-testing/spec.md")
	plan := read("../specs/002-deeper-testing/plan.md")
	tasks := read("../specs/002-deeper-testing/tasks.md")

	roadmap := read("product-roadmap.md")

	for path, content := range map[string]string{"spec": spec, "plan": plan, "tasks": tasks} {
		if !strings.Contains(content, "P5") || !strings.Contains(content, "deeper testing") {
			t.Errorf("%s does not define P5 deeper testing", path)
		}
	}
	for _, required := range []string{
		"race detector",
		"fuzz",
		"coverage",
		"integration",
		"no public MCP contract change",
	} {
		if !strings.Contains(spec, required) {
			t.Errorf("P5 spec does not contain %q", required)
		}
	}

	if !strings.Contains(roadmap, "| P4 targeted L1 hardening | Deployed |") {
		t.Error("roadmap does not mark P4 deployed")
	}
	if !strings.Contains(roadmap, "| P5 deeper testing | Deployed |") {
		t.Error("roadmap does not mark P5 deployed")
	}
}
