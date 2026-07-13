package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP7ClosureEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-13-p7.md")
	for _, required := range []string{
		"P7 closure baseline",
		"2e3245e920ae0d50c8814893f220575ec35203d1",
		"d1309ed08db0170e5165f78bf406e94cfa56cc11",
		"29280567173",
		"86920444713",
		"29281156750",
		"29281156767",
		"74.4%",
		"tool_count=62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"deployment UUID was not returned",
		"content-free JSONL",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P7 baseline does not contain %q", required)
		}
	}

	roadmap := read("product-roadmap.md")
	if !strings.Contains(roadmap, "| P7 structured observability | Deployed |") {
		t.Error("roadmap does not mark P7 deployed")
	}

	capsule := read("context-capsule.md")
	for _, required := range []string{
		"P7 structured observability is deployed",
		"p7-structured-observability",
		"authenticated dark console",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("capsule does not contain %q", required)
		}
	}

	spec := read("../specs/004-structured-observability/spec.md")
	plan := read("../specs/004-structured-observability/plan.md")
	tasks := read("../specs/004-structured-observability/tasks.md")
	for name, content := range map[string]string{"spec": spec, "plan": plan, "tasks": tasks} {
		if !strings.Contains(content, "Status: **complete**") {
			t.Errorf("%s does not mark P7 complete", name)
		}
	}
	if !strings.Contains(tasks, "[x] **T08 P7 closure**") {
		t.Error("tasks do not close T08")
	}
}
