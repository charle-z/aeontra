package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestCleanInstallAcceptanceUsesOnlyPublicLocalInputs(t *testing.T) {
	script, err := os.ReadFile("../scripts/verify-clean-install.sh")
	if err != nil {
		t.Fatal(err)
	}
	content := string(script)
	for _, marker := range []string{
		"CGO_ENABLED=0 go build -trimpath",
		"./cmd/mcp-devbox",
		"MCP_DEVBOX_STATE_ROOT",
		"--mode read-only",
		"--observability off",
		"protocolVersion",
		"read_file",
		"clean install fixture",
		"clean-install: PASS",
	} {
		if !strings.Contains(content, marker) {
			t.Errorf("clean-install acceptance missing %q", marker)
		}
	}
	for _, forbidden := range []string{
		"charle-z",
		"duckdns",
		"COOLIFY_",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"sudo ",
		"docker.sock",
	} {
		if strings.Contains(content, forbidden) {
			t.Errorf("clean-install acceptance depends on owner or privileged input %q", forbidden)
		}
	}
}
