package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubCLIPathPrefersFixedSafeExecutable(t *testing.T) {
	original := githubCLIPaths
	t.Cleanup(func() { githubCLIPaths = original })
	unsafe := filepath.Join(t.TempDir(), "unsafe-gh")
	if err := os.WriteFile(unsafe, []byte("unsafe"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	safe := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(safe, []byte("safe"), 0o755); err != nil {
		t.Fatal(err)
	}
	githubCLIPaths = []string{unsafe, safe}
	got, err := githubCLIPath()
	if err != nil || got != safe {
		t.Fatalf("path=%q err=%v", got, err)
	}
}

func TestGitHubConfigureReadsTokenOnlyFromStdinAndStatusIsSafe(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	var output bytes.Buffer
	if code := run([]string{"github", "configure", "--state", state, "--owner", "charle-z"}, strings.NewReader(token+"\n"), &output, &output); code != 0 {
		t.Fatalf("configure code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), token) || !strings.Contains(output.String(), `"configured":true`) || !strings.Contains(output.String(), `"owner":"charle-z"`) {
		t.Fatalf("unsafe configure output=%s", output.String())
	}
	output.Reset()
	if code := run([]string{"github", "status", "--state", state}, strings.NewReader(""), &output, &output); code != 0 {
		t.Fatalf("status code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), token) || !strings.Contains(output.String(), `"configured":true`) {
		t.Fatalf("unsafe status output=%s", output.String())
	}
}

func TestGitHubImportGHCopiesActiveLoginWithoutReturningToken(t *testing.T) {
	original := readGitHubCLIToken
	t.Cleanup(func() { readGitHubCLIToken = original })
	token := "github_pat_abcdefghijklmnopqrstuvwxyz0123456789"
	captured := []byte(token + "\n")
	readGitHubCLIToken = func() ([]byte, error) { return captured, nil }
	state := filepath.Join(t.TempDir(), "state")
	var output bytes.Buffer
	if code := run([]string{"github", "import-gh", "--state", state, "--owner", "charle-z"}, strings.NewReader("ignored"), &output, &output); code != 0 {
		t.Fatalf("import code=%d output=%s", code, output.String())
	}
	if strings.Contains(output.String(), token) || !strings.Contains(output.String(), `"configured":true`) || !strings.Contains(output.String(), `"owner":"charle-z"`) {
		t.Fatalf("unsafe import output=%s", output.String())
	}
	if bytes.Count(captured, []byte{0}) != len(captured) {
		t.Fatalf("captured GitHub CLI token was not cleared")
	}
	output.Reset()
	if code := run([]string{"github", "status", "--state", state}, strings.NewReader(""), &output, &output); code != 0 || strings.Contains(output.String(), token) {
		t.Fatalf("status code=%d output=%s", code, output.String())
	}
}

func TestGitHubImportGHFailsClosedWhenLoginIsUnavailable(t *testing.T) {
	original := readGitHubCLIToken
	t.Cleanup(func() { readGitHubCLIToken = original })
	readGitHubCLIToken = func() ([]byte, error) { return nil, errors.New("login missing") }
	var output bytes.Buffer
	if code := run([]string{"github", "import-gh", "--state", filepath.Join(t.TempDir(), "state"), "--owner", "charle-z"}, strings.NewReader(""), &output, &output); code == 0 {
		t.Fatalf("missing login accepted: %s", output.String())
	}
	if strings.Contains(output.String(), "login missing") {
		t.Fatalf("raw gh error leaked: %s", output.String())
	}
}

func TestGitHubImportGHUsesStoredLoginInsteadOfAmbientToken(t *testing.T) {
	filtered := filteredGitHubCLIEnvironment([]string{
		"HOME=/home/charles", "GH_TOKEN=secret-one", "GITHUB_TOKEN=secret-two",
		"GH_ENTERPRISE_TOKEN=secret-three", "GITHUB_ENTERPRISE_TOKEN=secret-four", "LANG=C.UTF-8",
	})
	joined := strings.Join(filtered, "\n")
	if strings.Contains(joined, "secret-") || !strings.Contains(joined, "HOME=/home/charles") || !strings.Contains(joined, "LANG=C.UTF-8") {
		t.Fatalf("filtered environment=%q", joined)
	}
}
