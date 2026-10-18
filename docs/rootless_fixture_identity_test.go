package docs_test

import (
	"os"
	"strings"
	"testing"
)

func TestRootlessPostgresFixtureIdentityIsDocumented(t *testing.T) {
	content, err := os.ReadFile("testing.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, required := range []string{
		"Rootless PostgreSQL fixture identity",
		"localhost/p12-postgres-fixture:<64 lowercase hexadecimal characters>",
		"podman image inspect",
		"P12_POSTGRES_IMAGE",
		"without permitting a network pull or arbitrary image reference",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("testing documentation missing %q", required)
		}
	}
}
