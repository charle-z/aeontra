//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type projectGitSyncRunner struct {
	head, remote    string
	status          string
	dirty           bool
	relation        string
	calls           []string
	credentialCalls []string
}

func (r *projectGitSyncRunner) Run(_ context.Context, _ string, args []string, credential edgeclient.GitHubCredential) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if credential.Token != "" {
		r.credentialCalls = append(r.credentialCalls, call)
	}
	switch call {
	case "rev-parse --verify HEAD":
		return r.head, nil
	case "branch --show-current":
		return "main", nil
	case "status --porcelain=v1 --untracked-files=all":
		if r.dirty {
			return " M dirty.go", nil
		}
		return r.status, nil
	case "remote get-url origin", "remote get-url --push origin":
		return "https://github.com/charle-z/repo.git", nil
	case "rev-parse --abbrev-ref --symbolic-full-name @{upstream}":
		return "origin/main", nil
	case "ls-remote --heads origin refs/heads/main":
		return r.remote + "\trefs/heads/main\n", nil
	case "rev-parse --verify refs/remotes/origin/main":
		return r.remote, nil
	case "rev-list --left-right --count " + r.head + "..." + r.remote:
		if r.relation == "" {
			return "0\t1", nil
		}
		return r.relation, nil
	case "merge-base --is-ancestor " + r.head + " " + r.remote:
		return "", nil
	case "fetch --no-tags origin refs/heads/main:refs/remotes/origin/main":
		return "", nil
	case "merge --ff-only " + r.remote:
		r.head = r.remote
		return "", nil
	default:
		return "", errors.New("unexpected Git command: " + call)
	}
}

func TestProjectGitFastForwardRejectsExpiredDivergedAndSymlinkedPlanState(t *testing.T) {
	resolved := projectGitResolution()
	credential := edgeclient.GitHubCredential{Owner: "charle-z"}
	now := time.Now().UTC()
	runner := &projectGitSyncRunner{head: "0123456789abcdef0123456789abcdef01234567", remote: "1123456789abcdef0123456789abcdef01234567"}
	stateRoot := t.TempDir()
	preview, err := previewProjectGitFastForward(context.Background(), stateRoot, resolved, runner, credential, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := executeProjectGitFastForward(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, credential, now.Add(projectGitPlanTTL+time.Second)); err == nil {
		t.Fatal("expired plan accepted")
	}

	runner.relation = "1\t1"
	if _, err := previewProjectGitFastForward(context.Background(), t.TempDir(), resolved, runner, credential, now); err == nil {
		t.Fatal("diverged checkout accepted")
	}

	unsafe := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(unsafe, "project-git-plans")); err != nil {
		t.Fatal(err)
	}
	runner.relation = "0\t1"
	if _, err := previewProjectGitFastForward(context.Background(), unsafe, resolved, runner, credential, now); err == nil {
		t.Fatal("symlinked plan root accepted")
	}
}

func projectGitResolution() edgeclient.ProjectResolution {
	return edgeclient.ProjectResolution{
		Project: edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"}, TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: "/work/project", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
	}
}

func TestInspectProjectGitCheckoutReportsOnlyBoundedRelation(t *testing.T) {
	runner := &projectGitSyncRunner{
		head: "0123456789abcdef0123456789abcdef01234567", remote: "1123456789abcdef0123456789abcdef01234567",
		status: "?? .mcp-devbox/runtime/home/.config/go/telemetry/local/weekends\n",
	}
	result, err := inspectProjectGitCheckout(context.Background(), projectGitResolution(), runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"})
	if err != nil || result.GitBranch != "main" || result.GitHead != runner.head || result.GitRemoteHead != runner.remote || result.GitAhead != 0 || result.GitBehind != 1 || !result.GitClean || result.GitDirty || !result.GitFetched {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "token") || strings.Contains(call, "--force") || strings.Contains(call, "--tags") {
			t.Fatalf("unsafe call: %s", call)
		}
	}
	if strings.Join(runner.credentialCalls, "\n") != "ls-remote --heads origin refs/heads/main" {
		t.Fatalf("credential calls=%v", runner.credentialCalls)
	}
}

func TestProjectGitFastForwardPlanIsExactSingleUseAndDirtyFailsClosed(t *testing.T) {
	stateRoot := t.TempDir()
	runner := &projectGitSyncRunner{head: "0123456789abcdef0123456789abcdef01234567", remote: "1123456789abcdef0123456789abcdef01234567"}
	resolved := projectGitResolution()
	preview, err := previewProjectGitFastForward(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z"}, time.Now().UTC())
	if err != nil || preview.GitPlanID == "" || preview.GitPlanExpiresAt == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	executed, err := executeProjectGitFastForward(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z"}, time.Now().UTC())
	if err != nil || !executed.GitFastForwarded || executed.GitHead != runner.remote {
		t.Fatalf("execute=%+v err=%v", executed, err)
	}
	if _, err := executeProjectGitFastForward(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z"}, time.Now().UTC()); err == nil {
		t.Fatal("replayed plan accepted")
	}
	runner.dirty = true
	if _, err := previewProjectGitFastForward(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z"}, time.Now().UTC()); err == nil {
		t.Fatal("dirty checkout accepted")
	}
}
