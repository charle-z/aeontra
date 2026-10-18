//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalProjectCheckoutInspectorClassifiesReadyDirtyAndRemoteDrift(t *testing.T) {
	repository := createProjectCheckoutFixture(t, "https://github.com/charle-z/ekoparty-trip-agent.git")
	inspector := localProjectCheckoutInspector{}
	state, err := inspector.Inspect(context.Background(), repository, "charle-z", "ekoparty-trip-agent")
	if err != nil || state != ProjectCheckoutReady {
		t.Fatalf("ready state=%s err=%v", state, err)
	}
	if err := os.WriteFile(filepath.Join(repository, "untracked.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = inspector.Inspect(context.Background(), repository, "charle-z", "ekoparty-trip-agent")
	if err != nil || state != ProjectCheckoutDirty {
		t.Fatalf("dirty state=%s err=%v", state, err)
	}
	if err := os.Remove(filepath.Join(repository, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, repository, "remote", "set-url", "origin", "https://github.com/charle-z/another.git")
	state, err = inspector.Inspect(context.Background(), repository, "charle-z", "ekoparty-trip-agent")
	if err != nil || state != ProjectCheckoutRemoteMismatch {
		t.Fatalf("remote state=%s err=%v", state, err)
	}
}

func TestLocalProjectCheckoutInspectorRejectsUnsafeMetadataAndPushRemote(t *testing.T) {
	repository := createProjectCheckoutFixture(t, "https://github.com/charle-z/repo.git")
	inspector := localProjectCheckoutInspector{}
	runProjectGit(t, repository, "remote", "set-url", "--push", "origin", "https://github.com/charle-z/other.git")
	state, err := inspector.Inspect(context.Background(), repository, "charle-z", "repo")
	if err != nil || state != ProjectCheckoutRemoteMismatch {
		t.Fatalf("push remote state=%s err=%v", state, err)
	}
	if err := os.RemoveAll(filepath.Join(repository, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(repository, ".git")); err != nil {
		t.Fatal(err)
	}
	state, err = inspector.Inspect(context.Background(), repository, "charle-z", "repo")
	if err == nil || state != ProjectCheckoutUnsafe {
		t.Fatalf("unsafe metadata state=%s err=%v", state, err)
	}
}

func TestProjectRemoteMatchesOnlyOwnerBoundHTTPSGitHubIdentity(t *testing.T) {
	for _, remote := range []string{
		"https://github.com/charle-z/repo.git",
		"https://GITHUB.com/CHARLE-Z/REPO.git",
	} {
		if !projectRemoteMatches(remote, "charle-z", "repo") {
			t.Fatalf("valid remote rejected: %s", remote)
		}
	}
	for _, remote := range []string{
		"git@github.com:charle-z/repo.git",
		"https://token@github.com/charle-z/repo.git",
		"https://github.com/other/repo.git",
		"https://github.com/charle-z/repo",
		"https://github.com/charle-z/repo.git?x=1",
		"https://github.com/charle-z/repo.git/extra",
	} {
		if projectRemoteMatches(remote, "charle-z", "repo") {
			t.Fatalf("unsafe remote accepted: %s", remote)
		}
	}
}

func createProjectCheckoutFixture(t *testing.T, remote string) string {
	t.Helper()
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, repository, "init")
	runProjectGit(t, repository, "config", "user.name", "MCP Devbox Test")
	runProjectGit(t, repository, "config", "user.email", "test@localhost")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, repository, "add", "README.md")
	runProjectGit(t, repository, "commit", "-m", "fixture")
	runProjectGit(t, repository, "remote", "add", "origin", remote)
	return repository
}

func runProjectGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, output)
	}
}
