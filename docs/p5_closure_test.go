package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5ClosureDocumentationIsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	roadmap := read("product-roadmap.md")
	tasks := read("../specs/002-deeper-testing/tasks.md")
	baseline := read("baselines/2026-07-13-p5.md")

	if !strings.Contains(roadmap, "| P5 deeper testing | Deployed |") {
		t.Error("roadmap does not mark P5 deployed")
	}
	if !strings.Contains(tasks, "- [x] **T08 P5 closure**") {
		t.Error("P5 tasks do not mark closure complete")
	}

	for _, required := range []string{
		"P5 closure baseline",
		"origin/main",
		"4a96307925751cf7fbe7a4f8eb801f86c8edc3ad",
		"5036d524ef69cf9bedf480a40afbf78dd7036b08",
		"race detector",
		"deterministic concurrency",
		"fuzz",
		"coverage gate",
		"integration matrix",
		"CGO_ENABLED=0",
		"P6",
		"62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"No publish, merge, or deploy",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P5 baseline does not contain %q", required)
		}
	}
}
