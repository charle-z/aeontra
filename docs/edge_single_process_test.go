package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestEdgeSingleProcessContractIsDocumented(t *testing.T) {
	content, err := os.ReadFile("edge-single-process.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"edge-instance.lock",
		"instance_lock_occupied",
		"stale_recoverable",
		"process=single",
		"process=duplicate",
		"coherence=managed",
		"coherence=manual",
		"edge_service_inactive",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("single-process documentation missing %q", required)
		}
	}
}
