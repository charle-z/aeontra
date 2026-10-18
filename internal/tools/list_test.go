package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestListDir_ShowsEntriesAndMarksGitRepos(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	repo := filepath.Join(root, "mcp-devbox")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, root, "notes.txt", "hello\n")
	write(t, root, ".env", "SECRET=hidden\n")

	out, err := svc.ListDir("")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mcp-devbox/ [git]", "notes.txt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list_dir missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".env") || strings.Contains(out, ".git") {
		t.Fatalf("list_dir should skip secret and ignored internals:\n%s", out)
	}
}

func TestListDir_DeniesOutsideWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeReadOnly)
	outside := filepath.Join(filepath.Dir(root), "outside-list")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListDir(outside); err == nil {
		t.Fatal("list_dir must not escape the workspace jail")
	}
}
