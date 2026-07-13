package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP6ClosureEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-13-p6.md")
	for _, required := range []string{
		"P6 closure baseline",
		"539e4d96c95aedd492ac36b428d4159054e183f4",
		"29272847130",
		"29272847139",
		"29273109759",
		"29273109780",
		"29273109419",
		"zero High/Critical",
		"tool_count=62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"deployment id was not returned",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P6 baseline does not contain %q", required)
		}
	}

	roadmap := read("product-roadmap.md")
	if !strings.Contains(roadmap, "| P6 CI/DevSecOps | Deployed |") {
		t.Error("roadmap does not mark P6 deployed")
	}

	capsule := read("context-capsule.md")
	for _, required := range []string{
		"P6 CI/DevSecOps is deployed",
		"p6-step92-closure",
		"P7 structured observability",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("capsule does not contain %q", required)
		}
	}

	spec := read("../specs/003-ci-devsecops/spec.md")
	plan := read("../specs/003-ci-devsecops/plan.md")
	tasks := read("../specs/003-ci-devsecops/tasks.md")
	for name, content := range map[string]string{"spec": spec, "plan": plan, "tasks": tasks} {
		if !strings.Contains(content, "Status: **complete**") {
			t.Errorf("%s does not mark P6 complete", name)
		}
	}
	for _, required := range []string{
		"[x] **T06 observed GitHub run**",
		"[x] **T07 P6 closure**",
	} {
		if !strings.Contains(tasks, required) {
			t.Errorf("tasks do not contain %q", required)
		}
	}
}
