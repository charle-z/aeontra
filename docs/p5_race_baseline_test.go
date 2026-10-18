package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5RaceBaselineIsDocumentedHonestly(t *testing.T) {
	content, err := os.ReadFile("testing.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"CGO_ENABLED=1 go test -race ./... -count=1",
		"CGO_ENABLED=0",
		"gcc",
		"blocked before tests executed",
		"must not be reported as green",
		"P6",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("testing.md does not contain %q", required)
		}
	}
}
