package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/config"
)

func TestRepoStatusReturnsRichFields(t *testing.T) {
	svc, root := initRepo(t, config.ModeReadOnly)
	configIdentity(t, root)
	write(t, root, "tracked.txt", "one\n")
	gitCmd(t, root, "add", "tracked.txt")
	gitCmd(t, root, "commit", "-qm", "base")
	write(t, root, "untracked.txt", "new\n")
	write(t, root, "with space.txt", "staged\n")
	gitCmd(t, root, "add", "with space.txt")
	write(t, root, "with space.txt", "modified after stage\n")

	out, err := svc.RepoStatus("")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"repository:", "branch:", "head:", "ahead:", "behind:", "staged_files:\n- with space.txt", "modified_files:\n- with space.txt", "untracked_files:", "untracked.txt", "clean: false", "detached_head: false"} {
		if !strings.Contains(out, want) {
			t.Errorf("rich status missing %q:\n%s", want, out)
		}
	}
}

func TestRepoFetchIsNarrowAndApprovalGated(t *testing.T) {
	svc, root := initRepo(t, config.ModeAsk)
	var calls [][]string
	svc.WithRunner(func(_ context.Context, _ string, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "fetched", nil
	})

	out, err := svc.RepoFetch(root, "", false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") || len(calls) != 0 {
		t.Fatalf("fetch before approval: out=%q err=%v calls=%v", out, err, calls)
	}
	if _, err := svc.RepoFetch(root, "--all", true); err == nil {
		t.Fatal("option-like remote must be rejected")
	}
	if _, err := svc.RepoFetch(filepath.Join(root, "..", "escape"), "origin", true); err == nil {
		t.Fatal("repo traversal must be rejected")
	}
	if _, err := svc.RepoFetch(root, "origin", true); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "fetch origin" {
		t.Fatalf("fetch argv = %#v", calls)
	}
}

func TestRepoFastForwardPlanRejectsReplayExpiryAndChangedState(t *testing.T) {
	svc, root := initFastForwardRepo(t, config.ModeAsk)
	preview, err := svc.RepoFastForwardPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	planID := field(preview, "plan_id")
	if planID == "" || !strings.Contains(preview, "git merge --ff-only origin/main") {
		t.Fatalf("bad preview:\n%s", preview)
	}
	out, err := svc.RepoFastForward(planID, false)
	if err != nil || !strings.Contains(out, "APPROVAL REQUIRED") {
		t.Fatalf("ask approval missing: out=%q err=%v", out, err)
	}
	if _, err := svc.RepoFastForward(planID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RepoFastForward(planID, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("replayed plan must fail: %v", err)
	}

	preview, err = svc.RepoFastForwardPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	changedPlan := field(preview, "plan_id")
	write(t, root, "changed.txt", "dirty\n")
	if _, err := svc.RepoFastForward(changedPlan, true); err == nil || !strings.Contains(err.Error(), "working tree") {
		t.Fatalf("dirty tree must invalidate plan: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "changed.txt")); err != nil {
		t.Fatal(err)
	}
	preview, err = svc.RepoFastForwardPreview(root)
	if err != nil {
		t.Fatal(err)
	}
	expired := field(preview, "plan_id")
	svc.plans.mu.Lock()
	svc.plans.plans[expired].ExpiresAt = time.Now().Add(-time.Minute)
	svc.plans.mu.Unlock()
	if _, err := svc.RepoFastForward(expired, true); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired plan must fail: %v", err)
	}
}

func TestRepoFastForwardPreviewRejectsDetachedDirtyDivergedAndOptionInjection(t *testing.T) {
	svc, root := initFastForwardRepo(t, config.ModeAllow)
	write(t, root, "dirty.txt", "dirty\n")
	if _, err := svc.RepoFastForwardPreview(root); err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("dirty preview must fail: %v", err)
	}
	_ = os.Remove(filepath.Join(root, "dirty.txt"))
	gitCmd(t, root, "checkout", "--detach", "-q")
	if _, err := svc.RepoFastForwardPreview(root); err == nil || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("detached preview must fail: %v", err)
	}
	if safeGitName("--upload-pack=evil") {
		t.Fatal("option-like git names must never validate")
	}

	svc, root = initFastForwardRepo(t, config.ModeAllow)
	write(t, root, "local.txt", "local\n")
	gitCmd(t, root, "add", "local.txt")
	gitCmd(t, root, "commit", "-qm", "local divergence")
	if _, err := svc.RepoFastForwardPreview(root); err == nil || !strings.Contains(err.Error(), "divergence") {
		t.Fatalf("non-fast-forward preview must fail: %v", err)
	}
}

func TestRepoFetchRejectsSymlinkEscape(t *testing.T) {
	svc, root := newTestService(t, config.ModeAllow)
	outside := t.TempDir()
	link := filepath.Join(root, "escaped-repo")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable on this host: %v", err)
	}
	if _, err := svc.RepoFetch("escaped-repo", "origin", true); err == nil {
		t.Fatal("symlink escape must be rejected")
	}
}

func TestRepoFastForwardRejectsChangedHeadAndTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{"head", func(t *testing.T, root string) {
			write(t, root, "local.txt", "x\n")
			gitCmd(t, root, "add", "local.txt")
			gitCmd(t, root, "commit", "-qm", "local")
		}, "HEAD changed"},
		{"target", func(t *testing.T, root string) { gitCmd(t, root, "update-ref", "refs/remotes/origin/main", "HEAD") }, "target changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, root := initFastForwardRepo(t, config.ModeAllow)
			preview, err := svc.RepoFastForwardPreview(root)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(t, root)
			if _, err := svc.RepoFastForward(field(preview, "plan_id"), true); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q rejection, got %v", tc.want, err)
			}
		})
	}
}

func initFastForwardRepo(t *testing.T, mode config.Mode) (*Service, string) {
	t.Helper()
	svc, root := initRepo(t, mode)
	configIdentity(t, root)
	write(t, root, "base.txt", "base\n")
	gitCmd(t, root, "add", "base.txt")
	gitCmd(t, root, "commit", "-qm", "base")
	gitCmd(t, root, "branch", "-M", "main")
	base := strings.TrimSpace(gitCmd(t, root, "rev-parse", "HEAD"))
	write(t, root, "remote.txt", "remote\n")
	gitCmd(t, root, "add", "remote.txt")
	gitCmd(t, root, "commit", "-qm", "remote")
	target := strings.TrimSpace(gitCmd(t, root, "rev-parse", "HEAD"))
	gitCmd(t, root, "reset", "--soft", base)
	gitCmd(t, root, "restore", "--staged", "remote.txt")
	_ = os.Remove(filepath.Join(root, "remote.txt"))
	gitCmd(t, root, "remote", "add", "origin", root)
	gitCmd(t, root, "update-ref", "refs/remotes/origin/main", target)
	gitCmd(t, root, "config", "branch.main.remote", "origin")
	gitCmd(t, root, "config", "branch.main.merge", "refs/heads/main")
	return svc, root
}

func field(text, key string) string {
	prefix := key + ": "
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
