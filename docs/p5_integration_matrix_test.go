package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5HermeticIntegrationMatrixIsDocumented(t *testing.T) {
	if _, err := os.Stat("../internal/integration/contracts_test.go"); err != nil {
		t.Fatalf("integration contract suite missing: %v", err)
	}
	content, err := os.ReadFile("testing.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"Hermetic integration matrix — P5 Step 83",
		"stdio/HTTP catalog parity",
		"bearer fail-closed",
		"OAuth challenge",
		"runtime identity",
		"local grant approval",
		"single-use",
		"planned note workflow",
		"loopback",
		"no external credentials",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("testing.md does not contain %q", required)
		}
	}
}
