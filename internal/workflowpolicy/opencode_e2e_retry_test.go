package workflowpolicy

import (
	"os"
	"strings"
	"testing"
)

func TestOpenCodeCombinedE2ERetryIsBoundedToExactNotFoundCategory(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/opencode-e2e.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, required := range []string{
		"run_combined_e2e()",
		"grep -Fxq 'E2E category=not_found'",
		"E2E bounded retry=not_found",
		"rm -f artifacts/opencode-combined-sandbox-report.json",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("opencode E2E workflow missing %q", required)
		}
	}
	if strings.Count(text, "run_combined_e2e") != 3 {
		t.Fatalf("combined E2E invocation count=%d want definition plus two bounded calls", strings.Count(text, "run_combined_e2e"))
	}
	for _, forbidden := range []string{"continue-on-error: true", "until run_combined_e2e", "while run_combined_e2e"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("opencode E2E workflow contains unbounded/nonblocking retry %q", forbidden)
		}
	}
}
