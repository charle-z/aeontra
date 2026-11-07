package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestTemporaryRuntimeRemediationIsBoundedAndRetires(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/runtime-remediation-watch.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("runtime-remediation-watch.yml", body); err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, value := range []string{"cron: '17 12 * * *'", "contents: read", "persist-credentials: false", "timeout-minutes: 5", "python3 scripts/check-runtime-remediation-upstream.py"} {
		if !strings.Contains(text, value) {
			t.Errorf("missing retirement control %q", value)
		}
	}
	for _, value := range []string{"contents: write", "packages: write", "secrets.", "continue-on-error:", "pull_request:", "git push", "platform_deploy"} {
		if strings.Contains(text, value) {
			t.Errorf("retirement watch has an unauthorized effect %q", value)
		}
	}
	body, err = os.ReadFile("../../.github/workflows/security.yml")
	if err != nil {
		t.Fatal(err)
	}
	text = string(body)
	if strings.Count(text, "--runtime-policy security/runtime-remediation.json") != 6 || strings.Contains(text, "--annotation-file Dockerfile.sandbox-runner --runtime-policy") {
		t.Fatal("runtime evidence must cover exactly six APK images and never scratch runner")
	}
	if !strings.Contains(text, "python3 -B scripts/test-runtime-remediation.py") || !strings.Contains(text, "sh scripts/prove-runtime-remediation.sh") || !strings.Contains(text, "*runtime-proof.json") {
		t.Fatal("final image proof or its negative controls/evidence retention missing")
	}
}
