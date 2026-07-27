package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP2ClosureDocumentationIsCurrent(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	readme := read("../README.md")

	baseline := read("baselines/2026-07-13-p2.md")

	if !strings.Contains(readme, "## Main capabilities") || !strings.Contains(readme, "docs/baselines/") {
		t.Error("README must present current capabilities and delegate phase history to baselines")
	}

	for _, required := range []string{
		"P2 closure baseline",
		"origin/main",
		"0de426e088466a1421b527f8ce1bf83cb53bd2a9",
		"serviceCore",
		"RepositoryCapability",
		"GitCapability",
		"SourceCapability",
		"PlatformCapability",
		"ExecutionCapability",
		"62",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"No publish, merge, or deploy",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P2 baseline does not contain %q", required)
		}
	}
}
