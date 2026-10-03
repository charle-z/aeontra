//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
	runner := NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin")
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
