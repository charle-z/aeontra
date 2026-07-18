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
		`pipeline_status=("${PIPESTATUS[@]}")`,
		`test_status="${pipeline_status[0]}"`,
		`redactor_status="${pipeline_status[1]}"`,
		"P12_RUNTIME_ID_CYCLE_1",
		"P12_RUNTIME_ID_CYCLE_2",
		"Run two clean rootless cycles and restart",
		"TestTrustedLinuxWorkcellRootlessCleanupE2E",
		"trap cleanup EXIT",
		"setsid podman system service",
		"rootless-cycle-1-report.json",
		"rootless-cycle-2-report.json",
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

	e2eBody, err := os.ReadFile("../edgeclient/trusted_linux_workcell_rootless_cycles_e2e_test.go")
	if err != nil {
		t.Fatal(err)
	}
	e2e := string(e2eBody)
	for _, required := range []string{"--health-cmd", `[]string{"container", "pod", "network", "volume"}`, "p12RootlessRuntimeIDs"} {
		if !strings.Contains(e2e, required) {
			t.Errorf("rootless lifecycle E2E missing %q", required)
		}
	}
}
