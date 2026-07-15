package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP9ReleaseCandidateEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-14-p9.md")
	for _, required := range []string{
		"P9 release-candidate baseline",
		"https://github.com/charle-z/mcp-devbox/pull/4",
		"96f7ca15183271772aecbf2d0ac2cceb88e20e5d",
		"29306099092",
		"29306099088",
		"86999607319",
		"86999607324",
		"86999607269",
		"86999607322",
		"86999607234",
		"86999607242",
		"86999607239",
		"modernc.org/sqlite v1.53.0",
		"internal/brain 81.2%",
		"tool_count=67",
		"sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"cmd/brain-smoke",
		"cmd/mcp-catalog-smoke",
		"zero High/Critical",
		"no resident service",
		"Production smoke remains pending",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P9 baseline does not contain %q", required)
		}
	}

	for name, path := range map[string]string{
		"spec":         "../specs/006-brain/spec.md",
		"plan":         "../specs/006-brain/plan.md",
		"tasks":        "../specs/006-brain/tasks.md",
		"threat model": "../specs/006-brain/threat-model.md",
	} {
		content := read(path)
		if !strings.Contains(content, "Status: **complete / merge-ready**") {
			t.Errorf("%s does not mark P9 complete / merge-ready", name)
		}
	}

	tasks := read("../specs/006-brain/tasks.md")
	for _, required := range []string{
		"[x] **T08 P9 release-candidate verification**",
		"[x] **T09 P9 release-candidate closure**",
		"[ ] **T10 P9 production closure**",
		"[ ] **T11 post-P9 console spec**",
	} {
		if !strings.Contains(tasks, required) {
			t.Errorf("P9 tasks do not contain %q", required)
		}
	}

	roadmap := read("product-roadmap.md")
	if !strings.Contains(roadmap, "| Brain memory | Deployed |") {
		t.Error("roadmap does not mark Brain memory deployed")
	}

	capsule := read("context-capsule.md")
	for _, required := range []string{
		"P9 Brain is deployed",
		"4fbe1dda02351c632e67c0f10a5c5b314df745e2",
		"tagged",
		"p9",
		"67 tools",
	} {
		if !strings.Contains(strings.ToLower(capsule), strings.ToLower(required)) {
			t.Errorf("capsule does not contain %q", required)
		}
	}

	for name, path := range map[string]string{
		"AGENTS":       "../AGENTS.md",
		"current task": "../.agent-memory/current-task.md",
		"handoff":      "../.agent-memory/handoffs/latest.md",
	} {
		content := read(path)
		if !strings.Contains(strings.ToLower(content), "p9") ||
			!strings.Contains(content, "4fbe1dda02351c632e67c0f10a5c5b314df745e2") {
			t.Errorf("%s does not record the deployed P9 base", name)
		}
	}

	documentationMap := read("documentation-map.md")
	if !strings.Contains(documentationMap, "P9 release-candidate evidence") ||
		!strings.Contains(documentationMap, "2026-07-14-p9.md") {
		t.Error("documentation map does not identify P9 release-candidate evidence")
	}

	for _, path := range []string{
		"../README.md",
		"../AGENTS.md",
		"context-capsule.md",
		"product-roadmap.md",
		"../.agent-memory/current-task.md",
		"../.agent-memory/handoffs/latest.md",
	} {
		content := read(path)
		if strings.Contains(content, "P10 BIOS Operations Console is implemented") ||
			strings.Contains(content, "Edge Agent is deployed") {
			t.Errorf("post-P9 implementation was claimed early in %s", path)
		}
	}
}
