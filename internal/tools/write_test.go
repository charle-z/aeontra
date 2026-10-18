package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestCreateFile_AllowCreatesWithContent(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	msg, err := svc.CreateFile("pkg/new.go", "package pkg\n\nfunc New() {}\n", false)
	if err != nil {
		t.Fatalf("create: %v\n%s", err, msg)
	}
	got, err := os.ReadFile(filepath.Join(root, "pkg", "new.go"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if normalizeEOL(string(got)) != "package pkg\n\nfunc New() {}\n" {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestCreateFile_CreatesInSelectedRepoUnderWorkspace(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	repo := filepath.Join(root, "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init", "-q")

	if _, err := svc.CreateFileIn("demo", "src/app.go", "package src\n", false); err != nil {
		t.Fatalf("create in selected repo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repo, "src", "app.go"))
	if err != nil {
		t.Fatalf("file not created in selected repo: %v", err)
	}
	if normalizeEOL(string(got)) != "package src\n" {
		t.Fatalf("unexpected selected repo content: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "app.go")); !os.IsNotExist(err) {
		t.Fatalf("create_file should not write relative to root when repo is selected, stat err=%v", err)
	}
}

func TestCreateFile_NoTrailingNewline(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	if _, err := svc.CreateFile("a.txt", "one\ntwo", false); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if normalizeEOL(string(got)) != "one\ntwo" {
		t.Errorf("content with no trailing newline mismatch: %q", got)
	}
}

func TestCreateFile_RefusesOverwrite(t *testing.T) {
	svc, root := initRepo(t, config.ModeAllow)
	write(t, root, "exists.txt", "already here\n")
	if _, err := svc.CreateFile("exists.txt", "new content\n", false); err == nil {
		t.Error("create_file should refuse to overwrite an existing file")
	}
}

func TestCreateFile_ReadOnlyDenied(t *testing.T) {
	svc, _ := initRepo(t, config.ModeReadOnly)
	if _, err := svc.CreateFile("x.txt", "hi\n", true); err == nil {
		t.Error("create_file in read-only mode should be denied")
	}
}

func TestCreateFile_AskRequiresApproval(t *testing.T) {
	svc, root := initRepo(t, config.ModeAsk)
	msg, err := svc.CreateFile("y.txt", "hi\n", false) // not approved
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "y.txt")); statErr == nil {
		t.Error("file should NOT exist before approval")
	}
	if msg == "" {
		t.Error("expected an approval-required message")
	}
	if _, err := svc.CreateFile("y.txt", "hi\n", true); err != nil {
		t.Fatalf("approved create failed: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "y.txt")); statErr != nil {
		t.Error("file should exist after approval")
	}
}

func TestCreateFile_DeniesSecretPath(t *testing.T) {
	svc, _ := initRepo(t, config.ModeAllow)
	if _, err := svc.CreateFile(".env", "SECRET=x\n", true); err == nil {
		t.Error("creating a secret-named file should be denied")
	}
}

func TestCreateFile_DeniesJailEscape(t *testing.T) {
	svc, _ := initRepo(t, config.ModeAllow)
	if _, err := svc.CreateFile(filepath.Join("..", "escape.txt"), "pwn\n", true); err == nil {
		t.Error("creating a file outside the jail should be denied")
	}
}
