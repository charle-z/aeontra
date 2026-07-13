package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP6ObservedActionsFailuresAndDiagnosticsAreDocumented(t *testing.T) {
	content, err := os.ReadFile("testing.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"Observed GitHub Actions — P6 Step 90",
		"29260843017",
		"29260848623",
		"runner.temp",
		"actionlint@v1.7.12",
		"CodeQL passed",
		"High or Critical",
		"cmd/grype-gate",
		"GitHub annotations",
		"not lowered",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("testing.md does not contain %q", required)
		}
	}
}
