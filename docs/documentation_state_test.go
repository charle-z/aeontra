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
	gitignore := read("../.gitignore")

	roadmap := read("product-roadmap.md")
	reconciliation := read("baselines/2026-08-12-operational-reconciliation.md")
	dualEdge := read("baselines/2026-08-27-v1_2_24-dual-edge.md")
	releaseAcceptance := read("baselines/2026-08-27-v1_2_25-release.md")
	changelog := read("../CHANGELOG.md")
	versioning := read("versioning.md")

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
		".agent-memory/",
		"optional Brain",
		"docs/context-capsule.md",
	} {
		if !strings.Contains(constitution, required) {
			t.Errorf("constitution does not contain %q", required)
		}
	}
	for _, required := range []string{
		"v1.2.25 signed release and device update",
		"194eda433669ab449fd0587e25174fb9d374bec1",
		"33121917992",
		"diagnostic_unavailable_windows",
		"project-process-worker",
		"No live rollback was executed",
	} {
		if !strings.Contains(releaseAcceptance, required) {
			t.Errorf("v1.2.25 release baseline does not contain %q", required)
		}
	}
	if !strings.Contains(gitignore, "/.agent-memory/") {
		t.Error("repository does not ignore operator-local .agent-memory state")
	}

	for _, required := range []string{
		"## Status snapshot — 2026-07-18",
		"P0-P5 architecture, hardening, and deeper testing",
		"Deployed",
		"P5 deeper testing",
		"P6 CI/DevSecOps",
		"539e4d96c95aedd492ac36b428d4159054e183f4",
		"Console/showcase",
		"Deployed",
		"Brain memory",
		"Deployed",
		"Console 2.0 / P8.1",
		"Deployed",
		"Universal execution profiles",
		"Planned",
		"Edge agents",
		"Development and Trusted Linux Workcell validated",
		"P11.2 remote OpenCode relay",
	} {
		if !strings.Contains(roadmap, required) {
			t.Errorf("product roadmap does not contain %q", required)
		}
	}

	for _, required := range []string{
		"Last updated: 2026-08-27",
		"Codex harness",
		"P16 worktrees and parallel tasks",
		"P17 durable objective supervisor",
		"Managed image and asset broker",
		"Native Windows Edge",
	} {
		if !strings.Contains(roadmap, required) {
			t.Errorf("current product roadmap does not contain %q", required)
		}
	}
	for _, required := range []string{
		"v1.2.24 dual-Edge operational acceptance",
		"48e8deb6e45c104736291c4fe883771ec063696a",
		"project_checkout_unsafe",
		"No live rollback was executed",
	} {
		if !strings.Contains(dualEdge, required) {
			t.Errorf("dual-Edge baseline does not contain %q", required)
		}
	}
	for _, required := range []string{"## Unreleased", "## v1.2.24", "third-party notice assets"} {
		if !strings.Contains(changelog, required) {
			t.Errorf("changelog does not contain %q", required)
		}
	}
	for _, required := range []string{
		"vMAJOR.MINOR.PATCH",
		"## Release identities",
		"## Retention",
		"stable",
		"Removing a GitHub release",
		"Git tag",
	} {
		if !strings.Contains(versioning, required) {
			t.Errorf("versioning policy does not contain %q", required)
		}
	}
	for _, required := range []string{
		"f8d0a38af06527dcf59763c793bee81aca9dd044",
		"04c544b776ffca2071cb5b5a9951b8b32f423a36",
		"489a64f40cbbde014986ff130662a485f9513d6c",
		"PR #154",
		"No restart or Edge update was performed",
	} {
		if !strings.Contains(reconciliation, required) {
			t.Errorf("operational reconciliation baseline does not contain %q", required)
		}
	}

}
