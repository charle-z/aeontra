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
	agents := read("../AGENTS.md")
	capsule := read("context-capsule.md")
	baseline := read("baselines/2026-07-13-p1.md")

	for path, content := range map[string]string{
		"README.md": readme,
		"AGENTS.md": agents,
	} {
		if !strings.Contains(content, "62") {
			t.Errorf("%s does not state the current 62-tool catalog", path)
		}
	}
	if strings.Contains(readme, "59 deliberately annotated") {
		t.Error("README.md still claims the pre-P0 59-tool catalog")
	}
	if strings.Contains(agents, "51 annotated MCP tools") {
		t.Error("AGENTS.md still claims the historical 51-tool catalog")
	}

	for _, required := range []string{
		"P1 catalog modularization is complete",
		"p1-tool-catalog-runtime",
		"62 tools",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"merge-ready",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("context-capsule.md does not contain %q", required)
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
