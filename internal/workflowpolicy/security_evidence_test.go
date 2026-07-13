package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestSecurityEvidenceWorkflowContainsRequiredJobsAndActions(t *testing.T) {
	content, err := os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	for _, required := range []string{
		"codeql:",
		"dependency-review:",
		"container-evidence:",
		"security-events: write",
		"github/codeql-action/init@v4.37.0",
		"github/codeql-action/analyze@v4.37.0",
		"actions/dependency-review-action@v5.0.0",
		"fail-on-severity: moderate",
		"docker build --file Dockerfile --tag mcp-devbox:ci .",
		"anchore/sbom-action@v0.24.0",
		"output-file: sbom.spdx.json",
		"upload-artifact: false",
		"upload-release-assets: false",
		"anchore/scan-action@v7.4.0",
		"image: mcp-devbox:ci",
		"fail-build: true",
		"severity-cutoff: high",
		"output-format: json",
		"output-file: grype.json",
		"test -s sbom.spdx.json",
		"test -s grype.json",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("security.yml does not contain %q", required)
		}
	}

	for _, forbidden := range []string{
		"secrets.",
		"docker login",
		"push: true",
		"continue-on-error: true",
		"mcp-devbox-charlez.duckdns.org",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("security.yml contains forbidden %q", forbidden)
		}
	}
	if got := strings.Count(text, "timeout-minutes:"); got != 3 {
		t.Fatalf("timeout count = %d, want 3", got)
	}
}
