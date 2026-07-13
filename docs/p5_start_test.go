package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5DeeperTestingIsDefinedAndActive(t *testing.T) {
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
	capsule := read("context-capsule.md")
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
	for _, required := range []string{
		"P4 targeted Layer-1 hardening is deployed",
		"4a96307925751cf7fbe7a4f8eb801f86c8edc3ad",
		"P5 deeper testing is active",
		"p5-deeper-testing",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("capsule does not contain %q", required)
		}
	}
	if !strings.Contains(roadmap, "| P4 targeted L1 hardening | Deployed |") {
		t.Error("roadmap does not mark P4 deployed")
	}
	if !strings.Contains(roadmap, "| P5 deeper testing | In progress |") {
		t.Error("roadmap does not mark P5 in progress")
	}
}
