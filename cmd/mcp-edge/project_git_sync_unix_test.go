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
	head, remote        string
	branch, upstream    string
	status              string
	dirty               bool
	relation            string
	trackingMissing     bool
	remoteObjectMissing bool
	published           bool
	calls               []string
	credentialCalls     []string
}

func (r *projectGitSyncRunner) Run(_ context.Context, _ string, args []string, credential edgeclient.GitHubCredential) (string, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if credential.Token != "" {
		r.credentialCalls = append(r.credentialCalls, call)
	}
	branch := r.branch
	if branch == "" {
		branch = "main"
	}
	upstream := r.upstream
	if upstream == "" && r.remote != "" {
		upstream = "origin/" + branch
	}
	switch call {
	case "rev-parse --verify HEAD":
		return r.head, nil
	case "branch --show-current":
		return branch, nil
	case "status --porcelain=v1 --untracked-files=normal":
		if r.dirty {
			return " M dirty.go", nil
		}
		return r.status, nil
	case "remote get-url origin", "remote get-url --push origin":
		return "https://github.com/charle-z/repo.git", nil
	case "for-each-ref --format=%(upstream:short) refs/heads/" + branch:
		return upstream, nil
	case "ls-remote --heads origin refs/heads/" + branch:
		if r.remote == "" {
			return "", nil
		}
		return r.remote + "\trefs/heads/" + branch + "\n", nil
	case "rev-parse --verify refs/remotes/origin/" + branch:
		if r.trackingMissing {
			return "", errors.New("tracking ref is unavailable")
		}
		return r.remote, nil
	case "rev-list --left-right --count " + r.head + "..." + r.remote:
		if r.remoteObjectMissing {
			return "", errors.New("remote object is unavailable")
		}
		if r.head == r.remote {
			return "0\t0", nil
		}
		if r.relation == "" {
			return "0\t1", nil
		}
		return r.relation, nil
	case "merge-base --is-ancestor " + r.head + " " + r.remote:
		return "", nil
	case "merge-base --is-ancestor " + r.remote + " " + r.head:
		if r.remoteObjectMissing {
			return "", errors.New("remote object is unavailable")
		}
		return "", nil
	case "fetch --no-tags origin refs/heads/" + branch + ":refs/remotes/origin/" + branch:
		return "", nil
	case "merge --ff-only " + r.remote:
		r.head = r.remote
		return "", nil
	case "push --porcelain --set-upstream origin " + branch + ":refs/heads/" + branch:
		r.remote = r.head
		r.upstream = "origin/" + branch
		r.trackingMissing = false
		r.published = true
		return "To https://github.com/charle-z/repo.git\n* refs/heads/" + branch + ":refs/heads/" + branch + " [new branch]", nil
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

func TestInspectProjectGitCheckoutAllowsCleanUnpublishedBranch(t *testing.T) {
	runner := &projectGitSyncRunner{
		head: "0123456789abcdef0123456789abcdef01234567", branch: "feat/download-maze-mvp",
	}
	result, err := inspectProjectGitCheckout(context.Background(), projectGitResolution(), runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"})
	if err != nil || result.GitBranch != runner.branch || result.GitHead != runner.head || result.GitRemoteHead != "" || !result.GitClean || result.GitFetched {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if strings.Join(runner.credentialCalls, "\n") != "ls-remote --heads origin refs/heads/"+runner.branch {
		t.Fatalf("credential calls=%v", runner.credentialCalls)
	}
}

func TestProjectGitPublishPlanIsExactSingleUseAndOwnerBound(t *testing.T) {
	stateRoot := t.TempDir()
	runner := &projectGitSyncRunner{
		head: "0123456789abcdef0123456789abcdef01234567", branch: "feat/download-maze-mvp",
	}
	resolved := projectGitResolution()
	preview, err := previewProjectGitPublish(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || preview.GitPlanID == "" || preview.GitPlanExpiresAt == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	executed, err := executeProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || !executed.GitPublished || executed.GitHead != runner.head || executed.GitRemoteHead != runner.head || !runner.published {
		t.Fatalf("execute=%+v published=%v err=%v", executed, runner.published, err)
	}
	if _, err := executeProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC()); err == nil {
		t.Fatal("replayed publication plan accepted")
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "--force") || strings.Contains(call, "--tags") || strings.Contains(call, "github_pat") {
			t.Fatalf("unsafe publication call: %s", call)
		}
	}
}

func TestProjectGitPublishAllowsLocallyProvableAdvanceWithoutCurrentTrackingRef(t *testing.T) {
	stateRoot := t.TempDir()
	runner := &projectGitSyncRunner{
		head: "1123456789abcdef0123456789abcdef01234567", remote: "0123456789abcdef0123456789abcdef01234567",
		branch: "feat/download-maze-mvp", relation: "1\t0", trackingMissing: true,
	}
	resolved := projectGitResolution()
	preview, err := previewProjectGitPublish(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || preview.GitPlanID == "" || preview.GitRemoteHead != runner.remote || preview.GitAhead != 1 || preview.GitBehind != 0 || preview.GitFetched {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	executed, err := executeProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil || !executed.GitPublished || executed.GitHead != runner.head || executed.GitRemoteHead != runner.head || !runner.published {
		t.Fatalf("execute=%+v published=%v err=%v", executed, runner.published, err)
	}
}

func TestProjectGitPublishRejectsRemoteCommitThatCannotBeProvedLocally(t *testing.T) {
	runner := &projectGitSyncRunner{
		head: "1123456789abcdef0123456789abcdef01234567", remote: "0123456789abcdef0123456789abcdef01234567",
		branch: "feat/download-maze-mvp", trackingMissing: true, remoteObjectMissing: true,
	}
	if preview, err := previewProjectGitPublish(context.Background(), t.TempDir(), projectGitResolution(), runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC()); err == nil || preview.GitPlanID != "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if runner.published {
		t.Fatal("publication ran without locally provable ancestry")
	}
}

func TestProjectGitPublishPlanRejectsRemoteStateChange(t *testing.T) {
	stateRoot := t.TempDir()
	runner := &projectGitSyncRunner{
		head: "0123456789abcdef0123456789abcdef01234567", branch: "feat/download-maze-mvp",
	}
	resolved := projectGitResolution()
	preview, err := previewProjectGitPublish(context.Background(), stateRoot, resolved, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	runner.remote = "1123456789abcdef0123456789abcdef01234567"
	if _, err := executeProjectGitPublish(context.Background(), stateRoot, resolved, preview.GitPlanID, runner, edgeclient.GitHubCredential{Owner: "charle-z", Token: "private"}, time.Now().UTC()); err == nil {
		t.Fatal("publication accepted a remote branch created after preview")
	}
	if runner.published {
		t.Fatal("publication ran after remote state changed")
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
