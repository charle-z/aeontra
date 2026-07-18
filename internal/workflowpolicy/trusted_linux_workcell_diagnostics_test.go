package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestTrustedLinuxWorkcellRootlessDiagnosticsAreFailureOnlyAndRedacted(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/trusted-linux-workcell-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"rootless-e2e-log-redactor",
		"status=${PIPESTATUS[0]}",
		"redactor_status=${PIPESTATUS[1]}",
		"Annotate rootless E2E failure",
		"Upload rootless failure diagnostic",
		"failure() && hashFiles('artifacts/p12-rootless-test.log') != ''",
		"rm -f artifacts/p12-rootless-test.log",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("rootless diagnostic workflow missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"tail -n 30 artifacts/p12-rootless-test.log",
		"artifacts/p12-podman-service.log\n          if-no-files-found",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("unsafe rootless diagnostic workflow contains %q", forbidden)
		}
	}
}
