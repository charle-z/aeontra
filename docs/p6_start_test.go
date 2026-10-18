package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP6CIDevSecOpsIsClosedAndDeployed(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/003-ci-devsecops/spec.md")
	plan := read("../specs/003-ci-devsecops/plan.md")
	tasks := read("../specs/003-ci-devsecops/tasks.md")

	roadmap := read("product-roadmap.md")

	for path, content := range map[string]string{"spec": spec, "plan": plan, "tasks": tasks} {
		if !strings.Contains(content, "P6") || !strings.Contains(content, "CI/DevSecOps") {
			t.Errorf("%s does not define P6 CI/DevSecOps", path)
		}
	}
	for _, required := range []string{
		"race detector",
		"coverage",
		"govulncheck",
		"CodeQL",
		"dependency review",
		"Scheduled fuzzing",
		"No active DAST against production",
		"No public MCP contract change",
	} {
		if !strings.Contains(spec, required) {
			t.Errorf("P6 spec does not contain %q", required)
		}
	}

	if !strings.Contains(roadmap, "| P5 deeper testing | Deployed |") {
		t.Error("roadmap does not mark P5 deployed")
	}
	if !strings.Contains(roadmap, "| P6 CI/DevSecOps | Deployed |") {
		t.Error("roadmap does not mark P6 deployed")
	}
}
