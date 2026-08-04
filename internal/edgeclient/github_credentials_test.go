package edgeclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubCredentialIsConfiguredFromStdinWithoutReturningToken(t *testing.T) {
	root := t.TempDir()
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	status, err := ConfigureGitHubCredential(root, "charle-z", strings.NewReader(token+"\n"))
	if err != nil || !status.Configured || status.Owner != "charle-z" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	encoded := status.Owner
	if strings.Contains(encoded, token) {
		t.Fatal("status exposed token")
	}
	info, err := os.Lstat(filepath.Join(root, githubCredentialFile))
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("credential mode=%v err=%v", info, err)
	}
	loaded, err := LoadGitHubCredential(root)
	if err != nil || loaded.Token != token || loaded.Owner != "charle-z" {
		t.Fatalf("loaded owner=%q token_match=%t err=%v", loaded.Owner, loaded.Token == token, err)
	}
}

func TestGitHubCredentialRejectsInvalidInputAndUnsafeFile(t *testing.T) {
	root := t.TempDir()
	for _, input := range []string{"short", "token with spaces 123456789012345", strings.Repeat("x", 1025)} {
		if _, err := ConfigureGitHubCredential(root, "charle-z", strings.NewReader(input)); err == nil {
			t.Fatalf("accepted token %q", input[:min(len(input), 20)])
		}
	}
	if _, err := ConfigureGitHubCredential(root, "bad/owner", strings.NewReader(strings.Repeat("x", 30))); err == nil {
		t.Fatal("accepted unsafe owner")
	}
	path := filepath.Join(root, githubCredentialFile)
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"owner":"charle-z","token":"github_pat_abcdefghijklmnopqrstuvwxyz"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGitHubCredential(root); err == nil {
		t.Fatal("accepted world-readable credential")
	}
}
