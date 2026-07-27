package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP7StructuredObservabilityIsDefinedAndDeployed(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/004-structured-observability/spec.md")
	plan := read("../specs/004-structured-observability/plan.md")
	tasks := read("../specs/004-structured-observability/tasks.md")
	threat := read("../specs/004-structured-observability/threat-model.md")
	operations := read("observability.md")

	roadmap := read("product-roadmap.md")
	readme := read("../README.md")
	baseline := read("baselines/2026-07-13-p7.md")

	for name, content := range map[string]string{
		"spec": spec, "plan": plan, "tasks": tasks, "threat model": threat,
	} {
		if !strings.Contains(content, "P7") || !strings.Contains(content, "structured observability") {
			t.Errorf("%s does not define P7 structured observability", name)
		}
	}

	if !strings.Contains(roadmap, "| P7 structured observability | Deployed |") {
		t.Error("roadmap does not mark P7 deployed")
	}
	if !strings.Contains(readme, "/version") || !strings.Contains(readme, "docs/baselines/") {
		t.Error("README must delegate live identity and historical release evidence")
	}
	if !strings.Contains(baseline, "P7 closure baseline") || !strings.Contains(baseline, "d1309ed08db0170e5165f78bf406e94cfa56cc11") {
		t.Error("P7 baseline does not identify the deployed closure")
	}
	for _, required := range []string{
		"MCP_DEVBOX_OBSERVABILITY",
		"MCP_DEVBOX_OBSERVABILITY_PATH",
		"MCP_DEVBOX_OBSERVABILITY_MAX_BYTES",
		"0700",
		"0600",
		"four total fixed segments",
		"X-MCP-Request-ID",
		"Rollback",
		"Troubleshooting",
	} {
		if !strings.Contains(operations, required) {
			t.Errorf("observability operations do not contain %q", required)
		}
	}
	for _, forbiddenBoundary := range []string{
		"prompt", "params", "response", "path", "target", "token", "raw error",
	} {
		if !strings.Contains(strings.ToLower(threat), forbiddenBoundary) {
			t.Errorf("threat model does not prohibit %q", forbiddenBoundary)
		}
	}
}
