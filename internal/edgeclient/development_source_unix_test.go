//go:build !windows

package edgeclient

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func developmentSourceFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("real Git is required for registered source evidence")
	}
	command := exec.CommandContext(t.Context(), gitPath, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
	command.Dir = dir
	command.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Git %q: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestRegisteredDevelopmentSourceEvidenceSupportsLargeRepositoryAndLeafLink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pinned registered-source leaf links require Linux")
	}
	for _, count := range []int{2, 7673} {
		t.Run(fmt.Sprintf("paths_%d", count), func(t *testing.T) {
			fixture := newProjectWorktreeFixture(t)
			for i := 0; i < count-2; i++ {
				if err := os.WriteFile(filepath.Join(fixture.canonical.Path, fmt.Sprintf("source-%05d.go", i)), []byte("package fixture\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("README.md", filepath.Join(fixture.canonical.Path, "source-link")); err != nil {
				t.Fatal(err)
			}
			developmentSourceFixtureGit(t, fixture.canonical.Path, "remote", "add", "origin", "https://github.com/charle-z/project.git")
			developmentSourceFixtureGit(t, fixture.canonical.Path, "add", ".")
			developmentSourceFixtureGit(t, fixture.canonical.Path, "commit", "--quiet", "-m", "test: add registered source topology")
			head := developmentSourceFixtureGit(t, fixture.canonical.Path, "rev-parse", "HEAD")
			registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: fixture.stateRoot, AllowedOwner: "charle-z", Workspaces: fixture.workspaces})
			if err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			if _, _, err := registry.Register(ProjectRegistration{Alias: "project", Owner: "charle-z", Repository: "project", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: fixture.canonical.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}}); err != nil {
				t.Fatal(err)
			}
			resolved, err := registry.Resolve(t.Context(), "project", "parrot")
			if err != nil {
				t.Fatal(err)
			}
			runner := NewRegisteredProjectSourceGitRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin")
			digest, observedHead, clean, err := RegisteredProjectSourceEvidence(t.Context(), registry, resolved, runner)
			if err != nil || !strings.HasPrefix(digest, "sha256:") || observedHead != head || !clean {
				t.Fatalf("source digest=%q head=%q clean=%v err=%v", digest, observedHead, clean, err)
			}
			repeated, _, _, err := RegisteredProjectSourceEvidence(t.Context(), registry, resolved, runner)
			if err != nil || repeated != digest {
				t.Fatalf("unstable source digest=%q err=%v", repeated, err)
			}
		})
	}
}

func TestDevelopmentSourceDigestTracksDirtyAndUntrackedSource(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: fixture.stateRoot, AllowedOwner: "charle-z", Workspaces: fixture.workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{Alias: "project", Owner: "charle-z", Repository: "project", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: fixture.canonical.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}}); err != nil {
		t.Fatal(err)
	}
	resolved, err := registry.Resolve(context.Background(), "project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	runner := NewRegisteredProjectSourceGitRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin")
	read := func() string {
		t.Helper()
		digest, err := RegisteredProjectContentDigest(context.Background(), registry, resolved, runner)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	initial := read()
	initialEvidence, initialHead, initialClean, err := RegisteredProjectSourceEvidence(context.Background(), registry, resolved, runner)
	if err != nil || initialEvidence != initial || !devGitCommitPattern.MatchString(initialHead) || !initialClean {
		t.Fatalf("clean source evidence=%q head=%q clean=%v err=%v", initialEvidence, initialHead, initialClean, err)
	}
	if err := os.WriteFile(filepath.Join(fixture.canonical.Path, "README.md"), []byte("ordinary development edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty := read()
	if dirty == initial {
		t.Fatal("dirty content was omitted from source evidence")
	}
	if err := os.WriteFile(filepath.Join(fixture.canonical.Path, "new.go"), []byte("package project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	untracked := read()
	if untracked == dirty || read() != untracked {
		t.Fatal("untracked source evidence is missing or unstable")
	}
	dirtyEvidence, dirtyHead, dirtyClean, err := RegisteredProjectSourceEvidence(context.Background(), registry, resolved, runner)
	if err != nil || dirtyEvidence != untracked || dirtyHead != initialHead || dirtyClean {
		t.Fatalf("dirty source evidence=%q head=%q clean=%v err=%v", dirtyEvidence, dirtyHead, dirtyClean, err)
	}
	invalid := resolved
	invalid.Project.ClaimGeneration++
	if _, err := RegisteredProjectContentDigest(context.Background(), registry, invalid, runner); err == nil {
		t.Fatal("stale repository generation produced valid evidence")
	}
}

func TestDevelopmentSourceDigestDoesNotTraverseSymlinks(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("outside-authority"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "source.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := projectSourceContentDigest(context.Background(), root, func([]string) ([]string, error) { return []string{"source.go"}, nil }, func() error { return nil }); err == nil {
		t.Fatal("source evidence followed a symlink outside the workspace")
	}
}
