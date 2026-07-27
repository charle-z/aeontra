package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP3ClosureDocumentationIsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-13-p3.md")

	for _, required := range []string{
		"P3 closure baseline",
		"origin/main",
		"ea332d173b4be1908bcf1c1abbe77ece610a6761",
		"cmd/mcp-devbox/main.go",
		"internal/app",
		"appRuntime",
		"parseServeOptions",
		"62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"No publish, merge, or deploy",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P3 baseline does not contain %q", required)
		}
	}
}
