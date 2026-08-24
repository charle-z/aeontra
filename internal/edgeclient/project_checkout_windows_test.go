//go:build windows

package edgeclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func windowsEnvironmentMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, raw, found := strings.Cut(value, "=")
		if found {
			result[key] = raw
		}
	}
	return result
}

func TestWindowsGitInspectionEnvironmentIsMinimal(t *testing.T) {
	values := windowsEnvironmentMap(windowsGitInspectionEnvironment())
	for _, key := range []string{"SystemRoot", "ComSpec", "PATHEXT", "PATH", "TEMP", "TMP", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_TERMINAL_PROMPT"} {
		if _, ok := values[key]; !ok {
			t.Errorf("inspection environment is missing %s", key)
		}
	}
	for _, forbidden := range []string{"MCP_DEVBOX_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "USERPROFILE", "SSH_AUTH_SOCK", "GIT_ASKPASS"} {
		if _, ok := values[forbidden]; ok {
			t.Errorf("inspection environment leaked %s", forbidden)
		}
	}
	if values["GIT_TERMINAL_PROMPT"] != "0" || values["GIT_CONFIG_GLOBAL"] != "NUL" || values["GIT_CONFIG_SYSTEM"] != "NUL" {
		t.Fatalf("inspection Git environment is not non-interactive and isolated: %#v", values)
	}
}

func TestWindowsGitCaptureBoundsCombinedOutput(t *testing.T) {
	capture := &windowsGitCapture{limit: 8}
	if written, err := capture.Write([]byte("0123456789")); err != nil || written != 10 {
		t.Fatalf("capture write=%d err=%v", written, err)
	}
	if got := capture.String(); got != "01234567" {
		t.Fatalf("capture=%q, want bounded output", got)
	}
	if !capture.truncated {
		t.Fatal("capture did not record truncation")
	}
}

func TestResolveWindowsGitPathUsesOnlyAbsoluteRegularExecutables(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveWindowsGitPath(filepath.Base(root)); err == nil {
		t.Fatal("relative Git search path accepted")
	}
	gitPath := filepath.Join(root, "git.exe")
	if err := os.WriteFile(gitPath, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveWindowsGitPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(resolved, gitPath) {
		t.Fatalf("resolved Git path=%q, want %q", resolved, gitPath)
	}
}

func TestWindowsGitEnvironmentScopesCredentialToBrokerProcess(t *testing.T) {
	token := "gho_test_token_1234567890"
	values := windowsEnvironmentMap(windowsGitEnvironment(`C:\Program Files\Git\cmd`, `C:\state\github-runtime`, `C:\state\github-runtime\tmp`, `C:\state\github-runtime\.askpass.cmd`, token))
	if values["MCP_DEVBOX_GITHUB_TOKEN"] != token {
		t.Fatal("broker token was not attached to the Git child environment")
	}
	if values["GIT_TERMINAL_PROMPT"] != "0" || values["GIT_CONFIG_NOSYSTEM"] != "1" || values["GIT_CONFIG_GLOBAL"] != "NUL" {
		t.Fatalf("Git credential environment is not isolated: %#v", values)
	}
	for _, key := range []string{"GITHUB_TOKEN", "GH_TOKEN", "SSH_AUTH_SOCK", "USERPROFILE"} {
		if _, ok := values[key]; ok {
			t.Errorf("Git child environment leaked %s", key)
		}
	}
}
