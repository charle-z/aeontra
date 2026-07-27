package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP1ClosureDocumentationIsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	readme := read("../README.md")

	baseline := read("baselines/2026-07-13-p1.md")

	for _, required := range []string{"docs/tools.md", "system_runtime_info", "/version"} {
		if !strings.Contains(readme, required) {
			t.Errorf("README.md does not point to current catalog identity via %q", required)
		}
	}
	for _, forbidden := range []string{"59 deliberately annotated", "current 67-tool catalog"} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README.md embeds historical catalog state %q", forbidden)
		}
	}

	for _, required := range []string{
		"P1 closure baseline",
		"origin/main",
		"3d161352b1d24670b07f48155f1eddc6370af8fd",
		"62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"No publish, merge, or deploy",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P1 baseline does not contain %q", required)
		}
	}
}
