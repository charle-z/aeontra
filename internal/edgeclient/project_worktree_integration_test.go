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

func TestProjectWorktreeIntegrationAncestryRequiresExactCleanHEAD(t *testing.T) {
	f := newProjectWorktreeFixture(t)
	m, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{StateRoot: f.stateRoot, Roots: f.roots, Workspaces: f.workspaces, Runner: NewDevGitCommandRunner(f.stateRoot, "/usr/local/bin:/usr/bin:/bin"), Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("a", 36)}})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	create := func(key, job string) ProjectWorktreeSnapshot {
		t.Helper()
		w, _, err := m.Create(context.Background(), ProjectWorktreeCreateRequest{Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project", CanonicalWorkspaceID: f.canonical.ID, CanonicalPath: f.canonical.Path, BaseCommit: f.head, Role: ProjectWorktreeWriter, JobID: job, LeaseID: "wl_11111111111111111111111111111111", Fence: 1, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	source := create("integration-source-0001", "wj_11111111111111111111111111111111")
	integrator := create("integration-review-0001", "wj_22222222222222222222222222222222")
	git := func(w ProjectWorktreeSnapshot, args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = w.path
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if err := os.WriteFile(filepath.Join(source.path, "source.txt"), []byte("source contribution\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(source, "add", "source.txt")
	git(source, "commit", "-m", "source contribution")
	sourceHead := git(source, "rev-parse", "HEAD")
	if _, err := m.StatusWithAncestors(context.Background(), integrator.ID, []string{sourceHead}); err == nil {
		t.Fatal("unincorporated source accepted")
	}
	for _, invalid := range [][]string{{"main"}, {"--force"}, {sourceHead, sourceHead}, {sourceHead, sourceHead, sourceHead, sourceHead, sourceHead}} {
		if _, err := m.StatusWithAncestors(context.Background(), integrator.ID, invalid); err == nil {
			t.Fatalf("invalid exact inputs accepted: %v", invalid)
		}
	}
	git(integrator, "merge", "--no-ff", "-m", "reviewed integration", sourceHead)
	status, err := m.StatusWithAncestors(context.Background(), integrator.ID, []string{sourceHead, f.head})
	if err != nil || !status.Clean || status.HeadCommit == f.head {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if got := git(source, "rev-parse", "HEAD"); got != sourceHead {
		t.Fatalf("source ref changed: %s", got)
	}
	if got := git(source, "status", "--porcelain"); got != "" {
		t.Fatalf("source tree changed: %s", got)
	}
	if err := os.WriteFile(filepath.Join(integrator.path, "pending.txt"), []byte("do not discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StatusWithAncestors(context.Background(), integrator.ID, []string{sourceHead}); err == nil {
		t.Fatal("dirty integration accepted")
	}
	if _, err := os.Stat(filepath.Join(integrator.path, "pending.txt")); err != nil {
		t.Fatal("inspection removed pending work")
	}
}

type integrationChangingRunner struct {
	DevGitCommandRunner
	path, ancestor string
}

func (r *integrationChangingRunner) Run(ctx context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	output, err := r.DevGitCommandRunner.Run(ctx, dir, args, credential)
	if err == nil && len(args) == 4 && args[0] == "merge-base" && args[2] == r.ancestor {
		command := exec.Command("git", "commit", "--allow-empty", "-m", "concurrent head change")
		command.Dir = r.path
		if data, changeErr := command.CombinedOutput(); changeErr != nil {
			return string(data), changeErr
		}
		r.ancestor = ""
	}
	return output, err
}

func TestProjectWorktreeIntegrationRejectsHEADChangeDuringProbe(t *testing.T) {
	m, w, _ := newProjectWorktreeForContentDigest(t)
	m.runner = &integrationChangingRunner{DevGitCommandRunner: m.runner, path: w.path, ancestor: w.BaseCommit}
	if _, err := m.StatusWithAncestors(context.Background(), w.ID, []string{w.BaseCommit}); err == nil {
		t.Fatal("mixed HEAD ancestry evidence accepted")
	}
}

func TestProjectWorktreeIntegrationConflictRemainsUnacceptedAndPreserved(t *testing.T) {
	m, w, _ := newProjectWorktreeForContentDigest(t)
	canonical, err := m.workspaces.Get(w.CanonicalWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	source, _, err := m.Create(context.Background(), ProjectWorktreeCreateRequest{Alias: w.Alias, TargetAlias: w.TargetAlias, Repository: w.Repository, CanonicalWorkspaceID: w.CanonicalWorkspaceID, CanonicalPath: canonical.Path, BaseCommit: w.BaseCommit, Role: ProjectWorktreeWriter, JobID: "wj_44444444444444444444444444444444", LeaseID: "wl_44444444444444444444444444444444", Fence: 1, IdempotencyKey: "integration-conflict-0001"})
	if err != nil {
		t.Fatal(err)
	}
	git := func(tree ProjectWorktreeSnapshot, args ...string) ([]byte, error) {
		command := exec.Command("git", args...)
		command.Dir = tree.path
		return command.CombinedOutput()
	}
	for i, tree := range []ProjectWorktreeSnapshot{source, w} {
		if err := os.WriteFile(filepath.Join(tree.path, "README.md"), []byte(strings.Repeat(string(rune('a'+i)), 3)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if data, err := git(tree, "add", "README.md"); err != nil {
			t.Fatalf("add: %v %s", err, data)
		}
		if data, err := git(tree, "commit", "-m", "independent conflicting change"); err != nil {
			t.Fatalf("commit: %v %s", err, data)
		}
	}
	sourceHeadBytes, err := git(source, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sourceHead := strings.TrimSpace(string(sourceHeadBytes))
	if data, err := git(w, "merge", "--no-ff", "-m", "review integration", sourceHead); err == nil {
		t.Fatalf("expected conflicting fixture: %s", data)
	}
	if _, err := m.StatusWithAncestors(context.Background(), w.ID, []string{sourceHead}); err == nil {
		t.Fatal("unresolved merge conflict accepted")
	}
	if data, err := git(w, "ls-files", "--unmerged"); err != nil || len(data) == 0 {
		t.Fatalf("conflict evidence discarded: %s %v", data, err)
	}
	if data, err := git(source, "rev-parse", "HEAD"); err != nil || strings.TrimSpace(string(data)) != sourceHead {
		t.Fatalf("source head changed: %s %v", data, err)
	}
}
