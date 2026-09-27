//go:build !windows

package edgeclient

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
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

func TestProjectWorktreeContentDigestCoversDirtyFilesAndExecutableMode(t *testing.T) {
	manager, worktree, authority := newProjectWorktreeForContentDigest(t)
	readme := filepath.Join(worktree.path, "README.md")

	initial, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || !strings.HasPrefix(initial, "sha256:") {
		t.Fatalf("initial digest=%q err=%v", initial, err)
	}
	repeated, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || repeated != initial {
		t.Fatalf("repeated digest=%q err=%v, want %q", repeated, err, initial)
	}

	if err := os.WriteFile(readme, []byte("modified tracked file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || modified == initial {
		t.Fatalf("tracked modification digest=%q err=%v, want a change", modified, err)
	}
	if err := os.Chmod(readme, 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || executable == modified {
		t.Fatalf("executable-mode digest=%q err=%v, want a change", executable, err)
	}

	untracked := filepath.Join(worktree.path, "notes.txt")
	if err := os.WriteFile(untracked, []byte("untracked content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withUntracked, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || withUntracked == executable {
		t.Fatalf("untracked digest=%q err=%v, want a change", withUntracked, err)
	}
	if err := os.Rename(untracked, filepath.Join(worktree.path, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	renamed, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || renamed == withUntracked {
		t.Fatalf("renamed digest=%q err=%v, want a change", renamed, err)
	}

	if err := os.Remove(readme); err != nil {
		t.Fatal(err)
	}
	deleted, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || deleted == renamed {
		t.Fatalf("deleted digest=%q err=%v, want a change", deleted, err)
	}

	ignore := filepath.Join(worktree.path, ".gitignore")
	if err := os.WriteFile(ignore, []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeIgnored, err := manager.ContentDigest(context.Background(), authority)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktree.path, "ignored.txt"), []byte("excluded by repository ignore rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterIgnored, err := manager.ContentDigest(context.Background(), authority)
	if err != nil || afterIgnored != beforeIgnored {
		t.Fatalf("ignored-file digest=%q err=%v, want unchanged %q", afterIgnored, err, beforeIgnored)
	}
}

func TestProjectWorktreeContentDigestRequiresCurrentFenceAndRejectsSymlinks(t *testing.T) {
	manager, worktree, authority := newProjectWorktreeForContentDigest(t)
	stale := authority
	stale.Fence++
	if _, err := manager.ContentDigest(context.Background(), stale); !errors.Is(err, ErrProjectWorktreeStaleFence) {
		t.Fatalf("stale authority error=%v, want %v", err, ErrProjectWorktreeStaleFence)
	}
	staleLease := authority
	staleLease.LeaseID = "wl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := manager.ContentDigest(context.Background(), staleLease); !errors.Is(err, ErrProjectWorktreeStaleFence) {
		t.Fatalf("stale lease error=%v, want %v", err, ErrProjectWorktreeStaleFence)
	}
	foreign := authority
	foreign.JobID = "wj_ffffffffffffffffffffffffffffffff"
	if _, err := manager.ContentDigest(context.Background(), foreign); !errors.Is(err, ErrProjectWorktreeConflict) {
		t.Fatalf("foreign authority error=%v, want %v", err, ErrProjectWorktreeConflict)
	}
	if err := os.Symlink("README.md", filepath.Join(worktree.path, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ContentDigest(context.Background(), authority); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("symlink error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
}

func TestProjectWorktreeContentPathListIsBoundedAndRejectsTraversal(t *testing.T) {
	traversal := []byte("../outside\x00")
	if _, err := parseProjectWorktreeContentPaths(traversal); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("traversal error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
	tooManyBytes := []byte(strings.Repeat("x", maxProjectWorktreeContentPathListBytes+1))
	if _, err := parseProjectWorktreeContentPaths(tooManyBytes); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("excessive path-list bytes error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
	tooMany := make([]byte, 0, maxProjectWorktreeContentPathListBytes)
	for index := 0; index <= maxProjectWorktreeContentFiles; index++ {
		tooMany = append(tooMany, 'f')
		tooMany = append(tooMany, strconv.AppendInt(nil, int64(index), 10)...)
		tooMany = append(tooMany, 0)
	}
	if _, err := parseProjectWorktreeContentPaths(tooMany); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("excessive path count error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
}

func TestProjectWorktreeContentDigestRejectsSpecialAndOversizedFiles(t *testing.T) {
	manager, worktree, authority := newProjectWorktreeForContentDigest(t)
	tracked := filepath.Join(worktree.path, "README.md")
	if err := os.Remove(tracked); err != nil {
		t.Fatal(err)
	}
	fifo := tracked
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ContentDigest(context.Background(), authority); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("FIFO error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	large := filepath.Join(worktree.path, "large.bin")
	file, err := os.OpenFile(large, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxProjectWorktreeContentBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ContentDigest(context.Background(), authority); !errors.Is(err, ErrProjectWorktreeUnsafe) {
		t.Fatalf("oversized file error=%v, want %v", err, ErrProjectWorktreeUnsafe)
	}
}

func TestProjectWorktreeContentDigestFailsClosedOnGitAndInventoryErrors(t *testing.T) {
	manager, _, authority := newProjectWorktreeForContentDigest(t)
	runner := manager.runner
	manager.runner = projectWorktreeContentFailingRunner{DevGitCommandRunner: runner, failCommand: "ls-tree"}
	if _, err := manager.ContentDigest(context.Background(), authority); !errors.Is(err, ErrProjectWorktreeUnavailable) {
		t.Fatalf("Git inventory error=%v, want %v", err, ErrProjectWorktreeUnavailable)
	}

	manager.runner = projectWorktreeContentFailingRunner{DevGitCommandRunner: runner, missingUntracked: "vanished.txt"}
	if _, err := manager.ContentDigest(context.Background(), authority); !errors.Is(err, ErrProjectWorktreeUnavailable) {
		t.Fatalf("missing enumerated untracked file error=%v, want %v", err, ErrProjectWorktreeUnavailable)
	}
}

type projectWorktreeContentFailingRunner struct {
	DevGitCommandRunner
	failCommand      string
	missingUntracked string
}

func (runner projectWorktreeContentFailingRunner) Run(ctx context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	if len(args) > 0 && args[0] == runner.failCommand && runner.failCommand != "" {
		return "", errors.New("injected Git inventory failure")
	}
	if len(args) > 0 && args[0] == "ls-files" && strings.Contains(strings.Join(args, " "), "--others") && runner.missingUntracked != "" {
		return runner.missingUntracked + "\x00", nil
	}
	return runner.DevGitCommandRunner.Run(ctx, dir, args, credential)
}

func newProjectWorktreeForContentDigest(t *testing.T) (*ProjectWorktreeManager, ProjectWorktreeSnapshot, ProjectWorktreeClaimRequest) {
	t.Helper()
	fixture := newProjectWorktreeFixture(t)
	manager, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{
		StateRoot: fixture.stateRoot, Roots: fixture.roots, Workspaces: fixture.workspaces,
		Runner:     NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("c", 36)},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	request := ProjectWorktreeCreateRequest{
		Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: fixture.head, Role: ProjectWorktreeWriter,
		JobID: "wj_0123456789abcdef0123456789abcdef", LeaseID: "wl_0123456789abcdef0123456789abcdef", Fence: 1,
		IdempotencyKey: "worktree-digest-01234567",
	}
	worktree, _, err := manager.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	authority := ProjectWorktreeClaimRequest{ID: worktree.ID, JobID: request.JobID, LeaseID: request.LeaseID, Fence: request.Fence}
	return manager, worktree, authority
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
