package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP4ClosureDocumentationIsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	capsule := read("context-capsule.md")
	roadmap := read("product-roadmap.md")
	tasks := read("../specs/001-layer-1/tasks.md")
	baseline := read("baselines/2026-07-13-p4.md")

	for _, required := range []string{
		"P4 targeted Layer-1 hardening is complete",
		"p4-l1-hardening",
		"merge-ready",
		"62 tools",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("context capsule does not contain %q", required)
		}
	}
	if strings.Contains(capsule, "P4 targeted Layer-1 hardening is active") {
		t.Error("context capsule still describes completed P4 as active")
	}
	if !strings.Contains(roadmap, "| P4 targeted L1 hardening | Complete / merge-ready |") {
		t.Error("roadmap does not mark P4 complete / merge-ready")
	}
	if !strings.Contains(tasks, "- [x] P4 targeted Layer-1 hardening") {
		t.Error("Layer 1 follow-on tasks do not mark P4 complete")
	}

	for _, required := range []string{
		"P4 closure baseline",
		"origin/main",
		"dd055e251c455086ddcb02bc302d9f406b05d6ce",
		"002bd783b76c83340eb9ab4075572a6e3f854117",
		"path-qualified command spoofing",
		"workspace-controlled executable",
		"grant TTL",
		"pending access requests",
		"audit file paths",
		"HTTP JSON-RPC batches",
		"62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"No publish, merge, or deploy",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P4 baseline does not contain %q", required)
		}
	}
}
