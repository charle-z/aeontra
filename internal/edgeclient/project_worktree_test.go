//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectWorktreeLifecycleUsesExactBaseAndFencedOwner(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	manager, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{
		StateRoot:  fixture.stateRoot,
		Roots:      fixture.roots,
		Workspaces: fixture.workspaces,
		Runner:     NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("a", 36)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	request := ProjectWorktreeCreateRequest{
		Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: fixture.head, Role: ProjectWorktreeWriter,
		JobID:   "wj_0123456789abcdef0123456789abcdef",
		LeaseID: "wl_0123456789abcdef0123456789abcdef", Fence: 1,
		IdempotencyKey: "worktree-create-01234567",
	}
	created, reused, err := manager.Create(context.Background(), request)
	if err != nil || reused {
		t.Fatalf("create reused=%v err=%v", reused, err)
	}
	if !projectWorktreeIDPattern.MatchString(created.ID) || created.BaseCommit != fixture.head || !strings.HasPrefix(created.Branch, "codex/worktree-") || created.Role != ProjectWorktreeWriter || created.State != ProjectWorktreeReady || created.Fence != 1 {
		t.Fatalf("unexpected snapshot: %+v", created)
	}
	workspace, err := fixture.workspaces.Get(created.WorkspaceID)
	if err != nil || workspace.Path != created.path || workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev {
		t.Fatalf("workspace=%+v err=%v", workspace, err)
	}
	if info, err := os.Lstat(filepath.Join(created.path, ".git")); err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("managed worktree metadata is unsafe: %v %v", info, err)
	}

	repeated, reused, err := manager.Create(context.Background(), request)
	if err != nil || !reused || repeated.ID != created.ID {
		t.Fatalf("repeat=%+v reused=%v err=%v", repeated, reused, err)
	}
	if _, err := manager.Claim(ProjectWorktreeClaimRequest{ID: created.ID, JobID: request.JobID, LeaseID: "wl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Fence: 1}); err == nil {
		t.Fatal("same fence with another lease must fail")
	}
	claimed, err := manager.Claim(ProjectWorktreeClaimRequest{ID: created.ID, JobID: request.JobID, LeaseID: "wl_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Fence: 2})
	if err != nil || claimed.Fence != 2 {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if _, err := manager.Claim(ProjectWorktreeClaimRequest{ID: created.ID, JobID: request.JobID, LeaseID: request.LeaseID, Fence: 1}); err == nil {
		t.Fatal("stale fence must fail")
	}
	if err := os.WriteFile(filepath.Join(created.path, "worker.txt"), []byte("isolated change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workerGit := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = created.path
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("worktree git %v: %v: %s", args, err, output)
		}
	}
	workerGit("add", "worker.txt")
	workerGit("commit", "-m", "worker change")
	if status, err := manager.Status(context.Background(), created.ID); err != nil || status.Branch != created.Branch ||
		!status.EvidenceKnown || status.HeadCommit == fixture.head || !status.Clean || status.CommitsAheadBase != 1 || status.ChangedPathCount != 1 {
		t.Fatalf("committed writer worktree status=%+v err=%v", status, err)
	}

	dirtyPath := filepath.Join(created.path, "uncommitted.txt")
	if err := os.WriteFile(dirtyPath, []byte("preserve me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup := ProjectWorktreeCleanupRequest{ID: created.ID, JobID: request.JobID, LeaseID: claimed.LeaseID, Fence: claimed.Fence, IdempotencyKey: "worktree-cleanup-01234567"}
	if _, _, err := manager.Cleanup(context.Background(), cleanup); err == nil {
		t.Fatal("dirty worktree cleanup must fail closed")
	}
	if _, err := os.Stat(dirtyPath); err != nil {
		t.Fatalf("dirty evidence was removed: %v", err)
	}
	if err := os.Remove(dirtyPath); err != nil {
		t.Fatal(err)
	}
	removed, reused, err := manager.Cleanup(context.Background(), cleanup)
	if err != nil || reused || removed.State != ProjectWorktreeRemoved {
		t.Fatalf("cleanup=%+v reused=%v err=%v", removed, reused, err)
	}
	if _, err := os.Lstat(created.path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if _, err := fixture.workspaces.Get(created.WorkspaceID); err == nil {
		t.Fatal("removed worktree remained registered")
	}
	repeatedCleanup, reused, err := manager.Cleanup(context.Background(), cleanup)
	if err != nil || !reused || repeatedCleanup.State != ProjectWorktreeRemoved {
		t.Fatalf("repeat cleanup=%+v reused=%v err=%v", repeatedCleanup, reused, err)
	}
}

func TestProjectWorktreeRejectsChangedBaseAndForeignJob(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	manager, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{
		StateRoot: fixture.stateRoot, Roots: fixture.roots, Workspaces: fixture.workspaces,
		Runner:     NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("b", 36)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	request := ProjectWorktreeCreateRequest{
		Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: strings.Repeat("f", 40), Role: ProjectWorktreeWriter,
		JobID: "wj_0123456789abcdef0123456789abcdef", LeaseID: "wl_0123456789abcdef0123456789abcdef", Fence: 1,
		IdempotencyKey: "worktree-create-abcdefgh",
	}
	if _, _, err := manager.Create(context.Background(), request); err == nil {
		t.Fatal("changed base must fail")
	}
	request.BaseCommit = fixture.head
	created, _, err := manager.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Claim(ProjectWorktreeClaimRequest{ID: created.ID, JobID: "wj_ffffffffffffffffffffffffffffffff", LeaseID: "wl_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Fence: 2}); err == nil {
		t.Fatal("foreign job claim must fail")
	}
}

type projectWorktreeFixture struct {
	stateRoot  string
	roots      WorkspaceRoots
	workspaces *WorkspaceRegistry
	canonical  Workspace
	head       string
}

func newProjectWorktreeFixture(t *testing.T) projectWorktreeFixture {
	t.Helper()
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	devRoot := filepath.Join(root, "workspaces")
	htbRoot := filepath.Join(root, "htb")
	for _, path := range []string{stateRoot, devRoot, htbRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	canonical := filepath.Join(devRoot, "project")
	if err := os.Mkdir(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = canonical
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--initial-branch=main")
	git("config", "user.name", "MCP Devbox Test")
	git("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(canonical, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "fixture")
	head := git("rev-parse", "HEAD")
	roots := WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot}
	workspaces, err := OpenWorkspaceRegistryWithRoots(stateRoot, roots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaces.Close() })
	workspace, _, err := workspaces.AddProfile(canonical, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	return projectWorktreeFixture{stateRoot: stateRoot, roots: roots, workspaces: workspaces, canonical: workspace, head: head}
}
