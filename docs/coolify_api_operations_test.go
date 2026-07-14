package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestCoolifyAPIOperationsRemainDocumented(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	runbook := read("runbooks/coolify-api-operations.md")
	for _, required := range []string{
		"or wrapper is an implementation gap",
		"GET    /api/v1/applications/{uuid}/storages",
		"POST   /api/v1/applications/{uuid}/storages",
		"jqf7qz5ensoqtvl1tb197gcv",
		"mcp-devbox-brain",
		"\"mount_path\": \"/brain\"",
		"Omit `host_path`",
		"Never call storage `DELETE` during P9 closure",
		"update an existing variable with the supported `PATCH` operation",
		"only `401` or `403` justify asking the operator",
		"does not require",
		"/var/run/docker.sock",
	} {
		if !strings.Contains(runbook, required) {
			t.Errorf("Coolify API runbook does not contain %q", required)
		}
	}

	mapDoc := read("documentation-map.md")
	if !strings.Contains(mapDoc, "docs/runbooks/coolify-api-operations.md") {
		t.Error("documentation map does not reference the Coolify API runbook")
	}

	agents := read("../AGENTS.md")
	for _, required := range []string{
		"Before asking the operator for a manual platform action",
		"missing MCP wrapper is",
		"docs/runbooks/coolify-api-operations.md",
	} {
		if !strings.Contains(agents, required) {
			t.Errorf("AGENTS.md does not contain %q", required)
		}
	}

	deploy := read("deploy-coolify.md")
	if !strings.Contains(deploy, "docs/runbooks/coolify-api-operations.md") {
		t.Error("Coolify deployment guide does not reference the API operations runbook")
	}
}
