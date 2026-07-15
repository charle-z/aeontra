package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP81ReleaseCandidateEvidenceIsSynchronized(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	baseline := read("baselines/2026-07-14-p8_1.md")
	for _, required := range []string{
		"P8.1 release-candidate baseline",
		"console-2.0",
		"4fbe1dda02351c632e67c0f10a5c5b314df745e2",
		"c4240672c9abfbb352b7a6b8ea39d7ae0e519d22",
		"1f97c1f7c077752a435bdecd484229f0381dc306",
		"548da51448cf3bf0f9a5d77b4f6d94d2b0cc3b79",
		"fb66b175cee23b0211f307fe50bf5ae73a6bbd74",
		"f2df0202f36ae2ec9a8c279e5fad1789d555b991",
		"90d3e38018d7cc8cd1df1bd71c1050805626ed4e",
		"https://github.com/charle-z/mcp-devbox/pull/10",
		"29379017283",
		"29379017244",
		"87238400355",
		"87238400385",
		"87238400362",
		"87238400345",
		"87238590650",
		"87238400293",
		"87238400336",
		"vitest 4.1.0",
		"vite 7.3.5",
		"zero High/Critical",
		"tool_count=67",
		"sha256:33f2701c9ad992b6da19ffae513fa08b429e38ca2294cc624a46d86db32128ed",
		"?key=<MCP_DEVBOX_TOKEN>",
		"HTTP 401",
		"Secure",
		"HttpOnly",
		"SameSite=Strict",
		"MCP_DEVBOX_TASK_ROOT",
		"/state/tasks",
		"Server-Sent Events",
		"bytes / 4 (estimate)",
		"not_paired",
		"internal/oauth 86.1%",
		"internal/taskjournal 82.4%",
		"No merge or production success is claimed",
	} {
		if !strings.Contains(baseline, required) {
			t.Errorf("P8.1 baseline does not contain %q", required)
		}
	}

	for name, path := range map[string]string{
		"design":             "console-2.0/design-neo-bios.md",
		"mockup":             "console-2.0/mockup.html",
		"console operations": "console.md",
	} {
		content := read(path)
		if strings.TrimSpace(content) == "" {
			t.Errorf("%s is empty", name)
		}
	}

	roadmap := read("product-roadmap.md")
	if !strings.Contains(roadmap, "| Console 2.0 / P8.1 | Complete / merge-ready |") {
		t.Error("roadmap does not mark P8.1 complete / merge-ready")
	}

	capsule := read("context-capsule.md")
	for _, required := range []string{
		"P8.1 Console 2.0 is complete / merge-ready",
		"console-2.0",
		"query-string credentials return 401",
		"/state/tasks",
		"2026-07-14-p8_1.md",
	} {
		if !strings.Contains(strings.ToLower(capsule), strings.ToLower(required)) {
			t.Errorf("capsule does not contain %q", required)
		}
	}

	consoleDoc := read("console.md")
	for _, required := range []string{
		"React, TypeScript and Vite",
		"/console/auth/start",
		"/console/auth/callback",
		"/console/tasks",
		"/console/events",
		"/console/data",
		"No WebSockets",
		"Not paired",
	} {
		if !strings.Contains(strings.ToLower(consoleDoc), strings.ToLower(required)) {
			t.Errorf("console documentation does not contain %q", required)
		}
	}

	for name, path := range map[string]string{
		"AGENTS":       "../AGENTS.md",
		"README":       "../README.md",
		"current task": "../.agent-memory/current-task.md",
		"handoff":      "../.agent-memory/handoffs/latest.md",
	} {
		content := read(path)
		if !strings.Contains(strings.ToLower(content), "p8.1") || !strings.Contains(strings.ToLower(content), "merge-ready") {
			t.Errorf("%s does not record P8.1 merge-ready state", name)
		}
	}

	documentationMap := read("documentation-map.md")
	if !strings.Contains(documentationMap, "P8.1 release-candidate evidence") || !strings.Contains(documentationMap, "2026-07-14-p8_1.md") {
		t.Error("documentation map does not identify P8.1 release-candidate evidence")
	}

	for _, path := range []string{
		"../README.md", "../AGENTS.md", "context-capsule.md", "product-roadmap.md",
		"../.agent-memory/current-task.md", "../.agent-memory/handoffs/latest.md",
	} {
		content := strings.ToLower(read(path))
		for _, forbidden := range []string{"edge core is deployed", "parrot workcell is deployed", "web terminal is implemented", "durable autonomous agent is deployed"} {
			if strings.Contains(content, forbidden) {
				t.Errorf("out-of-scope capability %q claimed in %s", forbidden, path)
			}
		}
	}
}
