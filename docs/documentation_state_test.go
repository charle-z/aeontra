package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestProjectDocumentationStateIsConsistent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/001-layer-1/spec.md")
	plan := read("../specs/001-layer-1/plan.md")
	tasks := read("../specs/001-layer-1/tasks.md")
	constitution := read("../.specify/memory/constitution.md")
	capsule := read("context-capsule.md")
	roadmap := read("product-roadmap.md")
	handoff := read("../.agent-memory/handoffs/latest.md")

	for path, content := range map[string]string{
		"spec.md":         spec,
		"plan.md":         plan,
		"tasks.md":        tasks,
		"constitution.md": constitution,
	} {
		if !strings.Contains(content, "Layer 1") {
			t.Errorf("%s no longer identifies the Layer 1 baseline", path)
		}
	}
	if !strings.Contains(spec, "Status: **completed and evolved**") {
		t.Error("Layer 1 spec is not marked completed and evolved")
	}
	if !strings.Contains(plan, "Status: **completed; architecture evolved through P3**") {
		t.Error("Layer 1 plan does not distinguish the completed historical plan from current architecture")
	}
	for i := 1; i <= 17; i++ {
		id := "T"
		if i < 10 {
			id += "0"
		}
		id += string(rune('0' + i%10))
		if i >= 10 {
			id = "T" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		}
		if !strings.Contains(tasks, "- [x] **"+id) {
			t.Errorf("Layer 1 task %s is not marked complete", id)
		}
	}
	if strings.Contains(constitution, "This session builds **Layer 1 only**") {
		t.Error("constitution still freezes the project at the original Layer 1 session")
	}
	for _, required := range []string{
		"Phase status must be evidence-based",
		"specs/",
		".agent-memory/current-task.md",
		"docs/context-capsule.md",
	} {
		if !strings.Contains(constitution, required) {
			t.Errorf("constitution does not contain %q", required)
		}
	}

	for _, required := range []string{
		"P3 composition root is deployed",
		"dd055e251c455086ddcb02bc302d9f406b05d6ce",
		"P4 targeted Layer-1 hardening is active",
		"p4-l1-hardening",
		"Step 75",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("context capsule does not contain %q", required)
		}
	}
	if strings.Contains(capsule, "Review and merge `p3-composition-root`") {
		t.Error("context capsule still treats deployed P3 as pending merge")
	}

	for _, required := range []string{
		"## Status snapshot — 2026-07-13",
		"P0-P3 architecture foundations",
		"Deployed",
		"P4 targeted L1 hardening",
		"In progress",
		"Console/showcase",
		"Not started",
		"Universal execution profiles",
		"Planned",
		"Edge agents",
		"Planned; PC/WSL validation pending",
	} {
		if !strings.Contains(roadmap, required) {
			t.Errorf("product roadmap does not contain %q", required)
		}
	}

	for _, required := range []string{
		"p4-l1-hardening",
		"fe2e903",
		"documentation synchronization",
		"specs/001-layer-1",
		".specify/memory/constitution.md",
	} {
		if !strings.Contains(handoff, required) {
			t.Errorf("latest handoff does not contain %q", required)
		}
	}
}
