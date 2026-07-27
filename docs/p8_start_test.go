package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP8AuthenticatedDarkConsoleIsDefinedAndDeployed(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	spec := read("../specs/005-authenticated-dark-console/spec.md")
	plan := read("../specs/005-authenticated-dark-console/plan.md")
	tasks := read("../specs/005-authenticated-dark-console/tasks.md")
	threat := read("../specs/005-authenticated-dark-console/threat-model.md")
	operations := read("console.md")
	adr := read("adr/0002-p8-embedded-authenticated-console.md")

	roadmap := read("product-roadmap.md")
	readme := read("../README.md")

	for name, content := range map[string]string{
		"spec": spec, "plan": plan, "tasks": tasks, "threat model": threat,
	} {
		if !strings.Contains(content, "P8") || !strings.Contains(content, "authenticated dark console") {
			t.Errorf("%s does not define P8 authenticated dark console", name)
		}
	}

	if !strings.Contains(roadmap, "| Console/showcase | Deployed |") {
		t.Error("roadmap does not mark the console deployed")
	}
	if !strings.Contains(readme, "/console") || !strings.Contains(readme, "docs/baselines/") {
		t.Error("README must describe the authenticated console boundary and delegate release evidence")
	}

	for _, required := range []string{
		"/console/login",
		"MCP_DEVBOX_TOKEN",
		"HttpOnly",
		"SameSite=Strict",
		"Content-Security-Policy",
		"eight-hour expiry",
		"128 sessions",
		"Rollback",
		"Troubleshooting",
		"no new Coolify application",
	} {
		if !strings.Contains(operations, required) {
			t.Errorf("console operations do not contain %q", required)
		}
	}
	for _, required := range []string{
		"Status: Accepted",
		"Supersedes: ADR 0001",
		"existing Go HTTP application",
		"no new listener or Coolify application",
	} {
		if !strings.Contains(adr, required) {
			t.Errorf("P8 ADR does not contain %q", required)
		}
	}
	for _, forbiddenBoundary := range []string{
		"mcp tools", "repository", "prompt", "targets", "token", "audit", "deployment",
	} {
		if !strings.Contains(strings.ToLower(threat), forbiddenBoundary) {
			t.Errorf("threat model does not cover %q", forbiddenBoundary)
		}
	}
}
