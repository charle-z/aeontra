package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP9BrainIsDefinedAndReleaseReady(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/006-brain/spec.md")
	plan := read("../specs/006-brain/plan.md")
	tasks := read("../specs/006-brain/tasks.md")
	threat := read("../specs/006-brain/threat-model.md")
	adr := read("adr/0003-p9-markdown-truth-sqlite-fts5-cache.md")
	capsule := read("context-capsule.md")
	roadmap := read("product-roadmap.md")
	readme := read("../README.md")
	agents := read("../AGENTS.md")
	currentTask := read("../.agent-memory/current-task.md")
	handoff := read("../.agent-memory/handoffs/latest.md")

	for name, content := range map[string]string{
		"spec":         spec,
		"plan":         plan,
		"tasks":        tasks,
		"threat model": threat,
	} {
		if !strings.Contains(content, "P9 Brain") || !strings.Contains(content, "Status: **complete / merge-ready**") {
			t.Errorf("%s does not define merge-ready P9 Brain", name)
		}
	}

	for _, required := range []string{
		"MCP_DEVBOX_BRAIN_ROOT",
		"curated/",
		"working/",
		"author: owner",
		"agent:<name>",
		"provenance",
		"review_by",
		"[[slug]]",
		"10,000",
		"64 MiB",
		"4 KiB",
		"brain_search",
		"brain_read",
		"brain_write",
		"brain_index",
		"brain_context",
		"original 62",
		"no resident service",
	} {
		if !strings.Contains(spec, required) {
			t.Errorf("P9 spec does not contain %q", required)
		}
	}

	for _, required := range []string{
		"Status: Accepted",
		"modernc.org/sqlite@v1.53.0",
		"Markdown truth",
		"SQLite FTS5",
		"No embeddings",
		"4 GB RAM",
		"2 vCPU",
		"future hybrid",
	} {
		if !strings.Contains(strings.ToLower(adr), strings.ToLower(required)) {
			t.Errorf("ADR 0003 does not contain %q", required)
		}
	}

	for _, required := range []string{
		"Agent writes to `curated/`",
		"Secret enters persistent memory",
		"Path traversal through slug/link",
		"SQLite becomes source of truth",
		"Memory feedback loop",
		"no new resident service",
		"Stop conditions",
	} {
		if !strings.Contains(strings.ToLower(threat), strings.ToLower(required)) {
			t.Errorf("P9 threat model does not contain %q", required)
		}
	}

	for _, content := range []string{capsule, readme, agents, currentTask, handoff} {
		for _, required := range []string{
			"P8.1",
			"d343264bffdc0ae1bc045a9d723e913be977090c",
		} {
			if !strings.Contains(strings.ToLower(content), strings.ToLower(required)) {
				t.Errorf("current-state document does not contain %q", required)
			}
		}
	}

	if !strings.Contains(roadmap, "| Brain memory | Deployed |") {
		t.Error("roadmap does not mark Brain memory deployed")
	}
	if !strings.Contains(tasks, "[x] **T01 P9 definition**") {
		t.Error("P9 tasks do not complete T01")
	}
	if strings.Contains(capsule, "P10 Layer 2/3 is implemented") || strings.Contains(roadmap, "| Layer 2/3 egress | Deployed |") {
		t.Error("P10 implementation was started before P9 closure")
	}
}
