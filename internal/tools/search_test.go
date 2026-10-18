package tools

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestSearchCode_FindsMatches(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, "a.go", "package a\nfunc Hello() {}\n")
	write(t, root, "sub/b.go", "package sub\nvar Hello = 1\n")
	out, err := svc.SearchCode("Hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.go:2") || !strings.Contains(out, "sub/b.go:2") {
		t.Errorf("expected matches with file:line, got:\n%s", out)
	}
}

func TestSearchCode_SkipsSecretAndIgnoredDirs(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	write(t, root, ".env", "TARGETWORD=1")
	write(t, root, "node_modules/dep/x.js", "var TARGETWORD = 1")
	write(t, root, ".ssh/config", "TARGETWORD here")
	write(t, root, "real.go", "// TARGETWORD in real source\n")
	out, err := svc.SearchCode("TARGETWORD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "real.go") {
		t.Errorf("should find the match in real source: %s", out)
	}
	for _, leaked := range []string{".env", "node_modules", ".ssh"} {
		if strings.Contains(out, leaked) {
			t.Errorf("search surfaced a skipped path %q: %s", leaked, out)
		}
	}
}

func TestSearchCode_RedactsMatchedSecretValue(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	// A secret in normal source; searching for a common word hits the line.
	write(t, root, "cfg.go", "const token = \"gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz\" // token\n")
	out, err := svc.SearchCode("token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "gh"+"p_0123456789abcdefghijklmnopqrstuvwxyz") {
		t.Errorf("search leaked a secret value: %s", out)
	}
}

func TestSearchCode_InvalidRegex(t *testing.T) {
	svc, _ := newTestService(t, config.ModeReadOnly)
	if _, err := svc.SearchCode("(unclosed"); err == nil {
		t.Error("invalid regex should error")
	}
}

func TestSearchCode_DoesNotEscapeJail(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	// Create a sibling file outside root with the term; it must not appear.
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	_ = write(t, filepath.Dir(root), "outside.txt", "UNIQUETERM outside")
	_ = outside
	write(t, root, "inside.go", "// UNIQUETERM inside\n")
	out, err := svc.SearchCode("UNIQUETERM")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "outside") {
		t.Errorf("search escaped the jail: %s", out)
	}
}
