//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type windowsProjectGitSyncTestRunner struct {
	head, remote, branch, upstream string
	published                      bool
	calls                          []string
}

func (r *windowsProjectGitSyncTestRunner) Run(_ context.Context, _ string, args []string, credential edgeclient.GitHubCredential) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if credential.Token != "" && args[0] != "ls-remote" && args[0] != "fetch" && args[0] != "push" {
		return "", errors.New("unexpected credential")
	}
	branch := r.branch
	if branch == "" {
		branch = "feature"
	}
	switch call {
	case "rev-parse --verify HEAD":
		return r.head, nil
	case "branch --show-current":
		return branch, nil
	case "status --porcelain=v1 --untracked-files=normal":
		return "", nil
	case "remote get-url origin", "remote get-url --push origin":
		return "https://github.com/charle-z/repo.git", nil
	case "for-each-ref --format=%(upstream:short) refs/heads/" + branch:
		return r.upstream, nil
	case "ls-remote --heads origin refs/heads/" + branch:
		if r.remote == "" {
			return "", nil
		}
		return r.remote + "\trefs/heads/" + branch + "\n", nil
	case "rev-parse --verify refs/remotes/origin/" + branch:
		if r.remote == "" {
			return "", errors.New("tracking ref missing")
		}
		return r.remote, nil
	case "rev-list --left-right --count " + r.head + "..." + r.remote:
		if r.remote == "" || r.head == r.remote {
			return "0\t0", nil
		}
		return "1\t0", nil
	case "merge-base --is-ancestor " + r.remote + " " + r.head:
		return "", nil
	case "merge-base --is-ancestor " + r.head + " " + r.remote:
		return "", nil
	case "fetch --no-tags origin refs/heads/" + branch + ":refs/remotes/origin/" + branch:
		r.upstream = "origin/" + branch
		if r.remote == "" {
			r.remote = r.head
		}
		return "", nil
	case "merge --ff-only " + r.remote:
		r.head = r.remote
		return "", nil
	case "push --porcelain --set-upstream origin " + branch + ":refs/heads/" + branch:
		r.remote, r.upstream, r.published = r.head, "origin/"+branch, true
		return "", nil
	default:
		return "", errors.New("unexpected Git command: " + call)
	}
}

func windowsProjectGitTestResolution() edgeclient.ProjectResolution {
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"},
		TargetAlias: "windows",
		Workspace:   edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: `C:\work\project`, Profile: edgeclient.WorkspaceProfileWindowsWorkcell, Mode: edgeclient.WorkspaceModeDev},
	}
}

func TestWindowsProjectGitPublishPlanIsExactSingleUse(t *testing.T) {
	runner := &windowsProjectGitSyncTestRunner{head: "0123456789abcdef0123456789abcdef01234567"}
	resolved := windowsProjectGitTestResolution()
	stateRoot := t.TempDir()
	if err := edgeclient.PreparePrivateRoot(stateRoot); err != nil {
		t.Skipf("private Windows test root unavailable: %v", err)
	}
	preview, err := previewWindowsProjectGitPublish(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || preview.GitPlanID == "" || preview.GitPlanExpiresAt == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	executed, err := executeWindowsProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || !executed.GitPublished || !runner.published {
		t.Fatalf("execute=%+v published=%v err=%v", executed, runner.published, err)
	}
	if _, err := executeWindowsProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC()); err == nil {
		t.Fatal("replayed publication plan accepted")
	}
}

func TestWindowsProjectGitPlanRejectsExpiredPlan(t *testing.T) {
	runner := &windowsProjectGitSyncTestRunner{head: "0123456789abcdef0123456789abcdef01234567"}
	resolved := windowsProjectGitTestResolution()
	now := time.Now().UTC()
	stateRoot := t.TempDir()
	if err := edgeclient.PreparePrivateRoot(stateRoot); err != nil {
		t.Skipf("private Windows test root unavailable: %v", err)
	}
	preview, err := previewWindowsProjectGitPublish(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeWindowsProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, now.Add(windowsProjectGitPlanTTL+time.Second)); err == nil {
		t.Fatal("expired publication plan accepted")
	}
}
