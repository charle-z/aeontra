//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
	"golang.org/x/sys/windows"
)

const windowsProjectGitPlanTTL = 5 * time.Minute

const (
	windowsProjectGitPlanFastForward = "fast_forward"
	windowsProjectGitPlanPublish     = "publish"
)

// windowsProjectGitPlan is the exact-head, owner-bound capability issued by a
// preview. It is private to the Edge state root and is consumed before the
// external Git mutation, so a plan cannot be replayed.
type windowsProjectGitPlan struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	Action      string    `json:"action"`
	WorkspaceID string    `json:"workspace_id"`
	Alias       string    `json:"alias"`
	Target      string    `json:"target"`
	Branch      string    `json:"branch"`
	Head        string    `json:"head"`
	RemoteHead  string    `json:"remote_head"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"used"`
}

// Windows Git operations share the brokered, credential-backed runner used by
// project preparation. No ambient Git credential helper is ever consulted.
func executeWindowsProjectGitSync(ctx context.Context, stateRoot string, operation edge.Operation) (edge.OperationResult, string) {
	credential, workspaces, projects, _, code := openWindowsProjectControlState(stateRoot)
	if code != "" {
		return edge.OperationResult{}, code
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(ctx, operation.Request.Alias, operation.Request.TargetAlias)
	if err != nil {
		return edge.OperationResult{}, safeWindowsProjectFailure(err)
	}
	runner := edgeclient.NewDevGitCommandRunner(stateRoot, "")
	if runner == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileWindowsWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev || resolved.Project.Owner != credential.Owner {
		return edge.OperationResult{}, "project_git_sync_failed"
	}
	var result edge.OperationResult
	switch operation.Kind {
	case edge.OperationProjectGitStatus:
		result, err = inspectWindowsProjectGit(ctx, resolved, runner, credential)
	case edge.OperationProjectGitFetch:
		result, err = fetchWindowsProjectGit(ctx, resolved, runner, credential)
	case edge.OperationProjectGitFastForwardPreview:
		result, err = previewWindowsProjectGitFastForward(ctx, stateRoot, resolved, runner, credential, time.Now().UTC())
	case edge.OperationProjectGitFastForward:
		result, err = executeWindowsProjectGitFastForward(ctx, stateRoot, resolved, operation.Request.GitPlanID, runner, credential, time.Now().UTC())
	case edge.OperationProjectGitPublishPreview:
		result, err = previewWindowsProjectGitPublish(ctx, stateRoot, resolved, runner, credential, time.Now().UTC())
	case edge.OperationProjectGitPublish:
		result, err = executeWindowsProjectGitPublish(ctx, stateRoot, resolved, operation.Request.GitPlanID, runner, credential, time.Now().UTC())
	default:
		return edge.OperationResult{}, "operation_invalid"
	}
	if err != nil {
		return edge.OperationResult{}, "project_git_sync_failed"
	}
	return result, ""
}

func inspectWindowsProjectGit(ctx context.Context, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential) (edge.OperationResult, error) {
	result := windowsProjectGitMetadata(resolved)
	if runner == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileWindowsWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev || credential.Owner != resolved.Project.Owner {
		return edge.OperationResult{}, errors.New("project Git checkout is unavailable")
	}
	local := func(args ...string) (string, error) {
		output, err := runner.Run(ctx, resolved.Workspace.Path, args, edgeclient.GitHubCredential{})
		return strings.TrimSpace(output), err
	}
	remote := func(args ...string) (string, error) {
		output, err := runner.Run(ctx, resolved.Workspace.Path, args, credential)
		return strings.TrimSpace(output), err
	}
	head, err := local("rev-parse", "--verify", "HEAD")
	if err != nil || !windowsGitCommit(head) {
		return edge.OperationResult{}, errors.New("Windows Git HEAD is invalid")
	}
	result.GitHead = head
	branch, err := local("branch", "--show-current")
	if err != nil {
		return edge.OperationResult{}, errors.New("Windows Git branch is unavailable")
	}
	if branch == "" {
		result.GitDetached = true
	} else if !windowsGitBranch(branch) {
		return edge.OperationResult{}, errors.New("Windows Git branch is invalid")
	} else {
		result.GitBranch = branch
	}
	status, err := local(edgeclient.ProjectCheckoutStatusArgs()...)
	if err != nil {
		return edge.OperationResult{}, errors.New("Windows Git status is unavailable")
	}
	result.GitDirty = !edgeclient.ProjectCheckoutStatusClean(status)
	result.GitClean = !result.GitDirty
	if result.GitDetached {
		return result, nil
	}
	expected := "https://github.com/" + credential.Owner + "/" + resolved.Project.Repository + ".git"
	origin, err := local("remote", "get-url", "origin")
	if err != nil || origin != expected {
		return edge.OperationResult{}, errors.New("Windows Git remote is not owner-bound")
	}
	pushOrigin, err := local("remote", "get-url", "--push", "origin")
	if err != nil || pushOrigin != expected {
		return edge.OperationResult{}, errors.New("Windows Git push remote is not owner-bound")
	}
	upstream, err := local("for-each-ref", "--format=%(upstream:short)", "refs/heads/"+branch)
	if err != nil || (upstream != "" && upstream != "origin/"+branch) {
		return edge.OperationResult{}, errors.New("Windows Git upstream is invalid")
	}
	live, err := remote("ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return edge.OperationResult{}, errors.New("Windows Git remote HEAD is unavailable")
	}
	fields := strings.Fields(live)
	if len(fields) == 0 {
		return result, nil
	}
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch || !windowsGitCommit(fields[0]) {
		return edge.OperationResult{}, errors.New("Windows Git remote HEAD is invalid")
	}
	result.GitRemoteHead = fields[0]
	tracked, trackErr := local("rev-parse", "--verify", "refs/remotes/origin/"+branch)
	result.GitFetched = trackErr == nil && tracked == result.GitRemoteHead
	counts, countErr := local("rev-list", "--left-right", "--count", head+"..."+result.GitRemoteHead)
	if countErr != nil {
		return result, nil
	}
	parts := strings.Fields(counts)
	if len(parts) != 2 {
		return edge.OperationResult{}, errors.New("Windows Git relation is invalid")
	}
	ahead, aheadErr := strconv.Atoi(parts[0])
	behind, behindErr := strconv.Atoi(parts[1])
	if aheadErr != nil || behindErr != nil || ahead < 0 || behind < 0 {
		return edge.OperationResult{}, errors.New("Windows Git relation is invalid")
	}
	result.GitAhead, result.GitBehind, result.GitDiverged = ahead, behind, ahead > 0 && behind > 0
	return result, nil
}

func fetchWindowsProjectGit(ctx context.Context, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential) (edge.OperationResult, error) {
	before, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || before.GitDetached {
		return edge.OperationResult{}, errors.New("project Git fetch preflight failed")
	}
	refspec := "refs/heads/" + before.GitBranch + ":refs/remotes/origin/" + before.GitBranch
	if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"fetch", "--no-tags", "origin", refspec}, credential); err != nil {
		return edge.OperationResult{}, errors.New("project Git fetch failed")
	}
	after, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || !after.GitFetched {
		return edge.OperationResult{}, errors.New("project Git fetch verification failed")
	}
	after.GitFetched = true
	return after, nil
}

func previewWindowsProjectGitFastForward(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	status, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || status.GitDetached || !status.GitClean || !status.GitFetched || status.GitDiverged || status.GitAhead != 0 {
		return edge.OperationResult{}, errors.New("project Git checkout cannot fast-forward")
	}
	if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"merge-base", "--is-ancestor", status.GitHead, status.GitRemoteHead}, edgeclient.GitHubCredential{}); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward relation rejected")
	}
	id, err := newWindowsProjectGitPlanID()
	if err != nil {
		return edge.OperationResult{}, err
	}
	plan := windowsProjectGitPlan{Version: 1, ID: id, Action: windowsProjectGitPlanFastForward, WorkspaceID: resolved.Workspace.ID, Alias: resolved.Project.Alias, Target: resolved.TargetAlias, Branch: status.GitBranch, Head: status.GitHead, RemoteHead: status.GitRemoteHead, ExpiresAt: now.UTC().Add(windowsProjectGitPlanTTL)}
	if err := writeWindowsProjectGitPlan(stateRoot, plan, true); err != nil {
		return edge.OperationResult{}, err
	}
	status.GitPlanID, status.GitPlanExpiresAt = id, plan.ExpiresAt.Format(time.RFC3339)
	return status, nil
}

func executeWindowsProjectGitFastForward(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, planID string, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	plan, err := readWindowsProjectGitPlan(stateRoot, planID)
	if err != nil || plan.Action != windowsProjectGitPlanFastForward || plan.Used || !plan.ExpiresAt.After(now.UTC()) || plan.WorkspaceID != resolved.Workspace.ID || plan.Alias != resolved.Project.Alias || plan.Target != resolved.TargetAlias {
		return edge.OperationResult{}, errors.New("project Git fast-forward plan is unavailable")
	}
	status, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || !status.GitClean || !status.GitFetched || status.GitBranch != plan.Branch || status.GitHead != plan.Head || status.GitRemoteHead != plan.RemoteHead || status.GitDiverged || status.GitAhead != 0 {
		return edge.OperationResult{}, errors.New("project Git fast-forward state changed")
	}
	if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"merge-base", "--is-ancestor", status.GitHead, status.GitRemoteHead}, edgeclient.GitHubCredential{}); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward relation rejected")
	}
	if err := consumeWindowsProjectGitPlan(stateRoot, plan.ID); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward plan was already consumed")
	}
	plan.Used = true
	if err := writeWindowsProjectGitPlan(stateRoot, plan, false); err != nil {
		return edge.OperationResult{}, err
	}
	if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"merge", "--ff-only", plan.RemoteHead}, edgeclient.GitHubCredential{}); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward failed")
	}
	after, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || after.GitHead != plan.RemoteHead || after.GitRemoteHead != plan.RemoteHead || !after.GitClean {
		return edge.OperationResult{}, errors.New("project Git fast-forward verification failed")
	}
	after.GitFastForwarded = plan.Head != plan.RemoteHead
	return after, nil
}

func previewWindowsProjectGitPublish(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	status, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || status.GitDetached || !status.GitClean || status.GitDiverged || status.GitBehind != 0 {
		return edge.OperationResult{}, errors.New("project Git checkout cannot publish")
	}
	if status.GitRemoteHead != "" {
		if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"merge-base", "--is-ancestor", status.GitRemoteHead, status.GitHead}, edgeclient.GitHubCredential{}); err != nil {
			return edge.OperationResult{}, errors.New("project Git publication would not fast-forward")
		}
	}
	id, err := newWindowsProjectGitPlanID()
	if err != nil {
		return edge.OperationResult{}, err
	}
	plan := windowsProjectGitPlan{Version: 1, ID: id, Action: windowsProjectGitPlanPublish, WorkspaceID: resolved.Workspace.ID, Alias: resolved.Project.Alias, Target: resolved.TargetAlias, Branch: status.GitBranch, Head: status.GitHead, RemoteHead: status.GitRemoteHead, ExpiresAt: now.UTC().Add(windowsProjectGitPlanTTL)}
	if err := writeWindowsProjectGitPlan(stateRoot, plan, true); err != nil {
		return edge.OperationResult{}, err
	}
	status.GitPlanID, status.GitPlanExpiresAt = id, plan.ExpiresAt.Format(time.RFC3339)
	return status, nil
}

func executeWindowsProjectGitPublish(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, planID string, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	plan, err := readWindowsProjectGitPlan(stateRoot, planID)
	if err != nil || plan.Action != windowsProjectGitPlanPublish || plan.Used || !plan.ExpiresAt.After(now.UTC()) || plan.WorkspaceID != resolved.Workspace.ID || plan.Alias != resolved.Project.Alias || plan.Target != resolved.TargetAlias {
		return edge.OperationResult{}, errors.New("project Git publication plan is unavailable")
	}
	status, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || !status.GitClean || status.GitDetached || status.GitBranch != plan.Branch || status.GitHead != plan.Head || status.GitRemoteHead != plan.RemoteHead || status.GitDiverged || status.GitBehind != 0 {
		return edge.OperationResult{}, errors.New("project Git publication state changed")
	}
	if status.GitRemoteHead != "" {
		if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"merge-base", "--is-ancestor", status.GitRemoteHead, status.GitHead}, edgeclient.GitHubCredential{}); err != nil {
			return edge.OperationResult{}, errors.New("project Git publication would not fast-forward")
		}
	}
	if err := consumeWindowsProjectGitPlan(stateRoot, plan.ID); err != nil {
		return edge.OperationResult{}, errors.New("project Git publication plan was already consumed")
	}
	plan.Used = true
	if err := writeWindowsProjectGitPlan(stateRoot, plan, false); err != nil {
		return edge.OperationResult{}, err
	}
	refspec := plan.Branch + ":refs/heads/" + plan.Branch
	if _, err := runner.Run(ctx, resolved.Workspace.Path, []string{"push", "--porcelain", "--set-upstream", "origin", refspec}, credential); err != nil {
		return edge.OperationResult{}, errors.New("project Git publication failed")
	}
	after, err := inspectWindowsProjectGit(ctx, resolved, runner, credential)
	if err != nil || !after.GitClean || after.GitBranch != plan.Branch || after.GitHead != plan.Head || after.GitRemoteHead != plan.Head {
		return edge.OperationResult{}, errors.New("project Git publication verification failed")
	}
	after.GitPublished = true
	return after, nil
}

func windowsProjectGitMetadata(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: resolved.SafeState(), ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}
}

func newWindowsProjectGitPlanID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("project Git plan generation failed")
	}
	return "gp_" + hex.EncodeToString(raw), nil
}

func windowsProjectGitPlanPath(stateRoot, id string) (string, error) {
	if len(id) != 35 || !strings.HasPrefix(id, "gp_") || strings.IndexFunc(id[3:], func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) != -1 {
		return "", errors.New("project Git plan id is invalid")
	}
	root := filepath.Join(filepath.Clean(stateRoot), "project-git-plans")
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	workspace, err := edgeclient.OpenWindowsWorkcell(stateRoot, root)
	if err != nil {
		return "", errors.New("project Git plan root is unsafe")
	}
	_ = workspace.Close()
	return filepath.Join(root, id+".json"), nil
}

func writeWindowsProjectGitPlan(stateRoot string, plan windowsProjectGitPlan, create bool) error {
	path, err := windowsProjectGitPlanPath(stateRoot, plan.ID)
	if err != nil {
		return err
	}
	content, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	if create {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(append(content, '\n'))
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return errors.New("project Git plan write failed")
		}
		if err := edgeclient.ValidatePrivateRegularFile(path); err != nil {
			return errors.New("project Git plan ACL is unsafe")
		}
		return nil
	}
	temp := path + ".tmp"
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(content, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(temp)
		return errors.New("project Git plan update failed")
	}
	if err := edgeclient.ValidatePrivateRegularFile(temp); err != nil {
		_ = os.Remove(temp)
		return errors.New("project Git plan ACL is unsafe")
	}
	tempPtr, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	if err := windows.MoveFileEx(tempPtr, pathPtr, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		_ = os.Remove(temp)
		return errors.New("project Git plan update failed")
	}
	return nil
}

func readWindowsProjectGitPlan(stateRoot, id string) (windowsProjectGitPlan, error) {
	path, err := windowsProjectGitPlanPath(stateRoot, id)
	if err != nil {
		return windowsProjectGitPlan{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 4096 {
		return windowsProjectGitPlan{}, errors.New("project Git plan is unsafe")
	}
	if err := edgeclient.ValidatePrivateRegularFile(path); err != nil {
		return windowsProjectGitPlan{}, errors.New("project Git plan ACL is unsafe")
	}
	f, err := os.Open(path)
	if err != nil {
		return windowsProjectGitPlan{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 4096))
	dec.DisallowUnknownFields()
	var plan windowsProjectGitPlan
	if err := dec.Decode(&plan); err != nil {
		return windowsProjectGitPlan{}, errors.New("project Git plan is invalid")
	}
	remoteValid := (plan.Action == windowsProjectGitPlanPublish && plan.RemoteHead == "") || windowsGitCommit(plan.RemoteHead)
	if dec.Decode(&struct{}{}) != io.EOF || plan.Version != 1 || plan.ID != id || plan.ExpiresAt.IsZero() ||
		(plan.Action != windowsProjectGitPlanFastForward && plan.Action != windowsProjectGitPlanPublish) || !windowsGitCommit(plan.Head) || !remoteValid || !windowsGitBranch(plan.Branch) ||
		plan.WorkspaceID == "" || plan.Alias == "" || plan.Target == "" {
		return windowsProjectGitPlan{}, errors.New("project Git plan is invalid")
	}
	return plan, nil
}

func consumeWindowsProjectGitPlan(stateRoot, id string) error {
	path, err := windowsProjectGitPlanPath(stateRoot, id)
	if err != nil {
		return err
	}
	marker := path + ".consumed"
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString("consumed\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return edgeclient.ValidatePrivateRegularFile(marker)
}
