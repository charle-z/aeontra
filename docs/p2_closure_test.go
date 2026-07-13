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
	agents := read("../AGENTS.md")
	capsule := read("context-capsule.md")
	baseline := read("baselines/2026-07-13-p2.md")

	for path, content := range map[string]string{
		"README.md": readme,
		"AGENTS.md": agents,
	} {
		if !strings.Contains(content, "capability services") {
			t.Errorf("%s does not describe the P2 capability service architecture", path)
		}
	}

	for _, required := range []string{
		"P1 catalog modularization is deployed",
		"P2 capability service split is complete",
		"p2-capability-services",
		"62 tools",
		"sha256:e3f0b46c65d3ff85f6820cfde88d522d8c7a8db52377e7f4a40bce2dd6330b9c",
		"merge-ready",
	} {
		if !strings.Contains(capsule, required) {
			t.Errorf("context-capsule.md does not contain %q", required)
		}
	}
	if strings.Contains(capsule, "P1 catalog modularization is complete on branch") {
		t.Error("context-capsule.md still describes deployed P1 as an unmerged branch")
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
