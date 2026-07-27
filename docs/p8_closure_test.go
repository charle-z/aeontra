package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP8ClosureEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-13-p8.md")
	for _, required := range []string{
		"P8 closure baseline",
		"5f4ffb7d86857759342fc9883149c2dbe1a0030f",
		"7cd3e450f5b09744a6eae0b1b0d896d50b5a1968",
		"605a56d48a495f3c8a2ce62471223187ef2f5685",
		"https://github.com/charle-z/mcp-devbox/pull/2",
		"29290411676",
		"29290411679",
		"29290609147",
		"29290609178",
		"84.3%",
		"console smoke passed",
		"tool_count=62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"route=console",
		"deployment UUID was not returned",
		"no new resident service",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P8 baseline does not contain %q", required)
		}
	}

	roadmap := read("product-roadmap.md")
	if !strings.Contains(roadmap, "| Console/showcase | Deployed |") {
		t.Error("roadmap does not mark the console deployed")
	}

	spec := read("../specs/005-authenticated-dark-console/spec.md")
	plan := read("../specs/005-authenticated-dark-console/plan.md")
	tasks := read("../specs/005-authenticated-dark-console/tasks.md")
	for name, content := range map[string]string{"spec": spec, "plan": plan, "tasks": tasks} {
		if !strings.Contains(content, "Status: **complete**") {
			t.Errorf("%s does not mark P8 complete", name)
		}
	}
	if !strings.Contains(tasks, "[x] **T08 P8 closure**") {
		t.Error("tasks do not close T08")
	}

	documentationMap := read("documentation-map.md")
	if !strings.Contains(documentationMap, "P8 closure evidence") {
		t.Error("documentation map does not identify P8 closure evidence")
	}
}
