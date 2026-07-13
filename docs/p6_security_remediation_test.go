package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP6SecurityRemediationAndConnectorRunbookAreDocumented(t *testing.T) {
	report, err := os.ReadFile("security-reports/2026-07-13-p6-ci-container-findings.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"29263139285",
		"29263139756",
		"GO-2026-5856",
		"CVE-2026-58469",
		"CVE-2026-58471",
		"CVE-2026-58472",
		"GHSA-52v5-jr5w-gjxr",
		"GHSA-c2c7-rcm5-vvqj",
		"Go 1.26.5",
		"npm@12.0.1",
		"BusyBox",
		"No finding was ignored",
	} {
		if !strings.Contains(string(report), required) {
			t.Errorf("security report does not contain %q", required)
		}
	}

	runbook, err := os.ReadFile("runbooks/client-connector-reliability.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Expected MCP restart",
		"VPS saturation",
		"Tool timeout",
		"Coolify/build/deployment failure",
		"ChatGPT client or transport",
		"OOM",
		"deployment_id",
		"catalog_hash",
		"Do not blame",
	} {
		if !strings.Contains(string(runbook), required) {
			t.Errorf("connector runbook does not contain %q", required)
		}
	}
}
