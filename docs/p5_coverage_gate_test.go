package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestP5CoverageGateIsDocumentedAndReproducible(t *testing.T) {
	content, err := os.ReadFile("testing.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"go test ./... -coverprofile=coverage.out -covermode=atomic -count=1",
		"go run ./cmd/coverage-gate --profile coverage.out",
		"internal/policy",
		"internal/mcpserver/catalog",
		"internal/oauth",
		"internal/audit",
		"internal/tools",
		"internal/app",
		"internal/grantadmin",
		"internal/workqueue",
		"package-specific",
		"missing package",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("testing.md does not contain %q", required)
		}
	}
}
