package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestGitHubActionsDiagnosticsContractIsDocumented(t *testing.T) {
	content, err := os.ReadFile("github-actions-diagnostics.md")
	if err != nil {
		t.Fatal(err)
	}
	document := string(content)
	for _, required := range []string{
		"GITHUB_TOKEN",
		"Actions: Read",
		"Checks: Read",
		"source_pull_request_failure_diagnostics",
		"source_pull_request_job_log",
		"next_offset",
		"maximum readable window per job: 16 MiB",
		"without `Authorization`",
		"No manual GitHub CLI setup is needed",
	} {
		if !strings.Contains(document, required) {
			t.Errorf("GitHub Actions diagnostics documentation missing %q", required)
		}
	}
	tools, err := os.ReadFile("tools.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source_pull_request_failure_diagnostics", "source_pull_request_job_log"} {
		if !strings.Contains(string(tools), "`"+name+"`") {
			t.Errorf("tool reference missing %s", name)
		}
	}
	mapping, err := os.ReadFile("documentation-map.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mapping), "docs/github-actions-diagnostics.md") {
		t.Fatal("documentation map does not reference GitHub Actions diagnostics")
	}
}
