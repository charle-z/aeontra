//go:build !windows

package edgeclient

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevWorkcellOffersOwnerBoundGitActionsOnlyWhenCredentialIsConfigured(t *testing.T) {
	fixture, workspace, prepared, lease, runtimeDir := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	without, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(without.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"], "devGit") {
		t.Fatal("development Git tools appeared without local credential")
	}
	if _, err := ConfigureGitHubCredential(fixture.state, "charle-z", strings.NewReader("github_pat_abcdefghijklmnopqrstuvwxyz0123456789\n")); err != nil {
		t.Fatal(err)
	}
	withCredential, err := fixture.launcher.linuxWorkcellProcessSpec(runtimeDir, workspace, prepared, filepath.Join(runtimeDir, openCodeDriverSocketName), lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	config := withCredential.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"]
	for _, expected := range []string{"devGitSocketPath", openCodeSandboxDevGitSocket, "workspace_dev_git_clone", "workspace_dev_publish_preview", "workspace_dev_publish"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("configured provider is missing %q", expected)
		}
	}
	if strings.Contains(config, "github_pat_") {
		t.Fatal("provider configuration exposed the GitHub token")
	}
}
