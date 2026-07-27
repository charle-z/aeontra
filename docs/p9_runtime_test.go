package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP9RuntimeOperationsAreDocumentedAndPackaged(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(content)
	}

	runbook := read("runbooks/brain-operations.md")
	deploy := read("deploy-coolify.md")
	configuration := read("configuration.md")
	dockerfile := read("../Dockerfile")
	readme := read("../README.md")

	for _, required := range []string{
		"MCP_DEVBOX_BRAIN_ROOT=/brain",
		"dedicated persistent volume",
		"brain-smoke",
		"curated/",
		"working/",
		".cache/brain.db",
		"0700",
		"0600",
		"10001:10001",
		"no remote",
		"Backup",
		"Restore",
		"Rollback",
		"Troubleshooting",
		"production remains P8",
	} {
		if !strings.Contains(strings.ToLower(runbook), strings.ToLower(required)) {
			t.Errorf("Brain runbook does not contain %q", required)
		}
	}

	for _, required := range []string{
		"COPY go.mod go.sum ./",
		"mkdir -p /repos /brain",
		"chown -R mcpdevbox:mcpdevbox /repos /brain",
		`VOLUME ["/repos", "/brain", "/state"]`,
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}

	for _, required := range []string{"configuration.md", "Brain is optional", "/brain"} {
		if !containsNormalizedProse(deploy, required) {
			t.Errorf("deploy guide does not delegate Brain configuration via %q", required)
		}
	}
	for _, required := range []string{"MCP_DEVBOX_BRAIN_ROOT", "/brain", "cmd/brain-smoke", "outside the repository jail"} {
		if !strings.Contains(configuration, required) {
			t.Errorf("configuration reference does not contain %q", required)
		}
	}
	for _, required := range []string{"docs/configuration.md", "/brain"} {
		if !strings.Contains(readme, required) {
			t.Errorf("README does not delegate Brain configuration via %q", required)
		}
	}

	for _, forbidden := range []string{
		"new Brain application",
		"Brain database server",
		"Brain worker service",
	} {
		if strings.Contains(runbook, forbidden) || strings.Contains(deploy, forbidden) {
			t.Errorf("runtime docs contain forbidden architecture %q", forbidden)
		}
	}
}
