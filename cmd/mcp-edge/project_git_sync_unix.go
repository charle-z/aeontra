//go:build !windows

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
	"syscall"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

const projectGitPlanTTL = 5 * time.Minute

type projectGitFastForwardPlan struct {
	Version     int       `json:"version"`
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Alias       string    `json:"alias"`
	Target      string    `json:"target"`
	Branch      string    `json:"branch"`
	Head        string    `json:"head"`
	RemoteHead  string    `json:"remote_head"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"used"`
}

func inspectProjectGitCheckout(ctx context.Context, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential) (edge.OperationResult, error) {
	result := projectGitMetadata(resolved)
	if runner == nil || resolved.Workspace.Profile != edgeclient.WorkspaceProfileLinuxWorkcell || resolved.Workspace.Mode != edgeclient.WorkspaceModeDev || credential.Owner != resolved.Project.Owner {
		return edge.OperationResult{}, errors.New("project Git checkout is unavailable")
	}
	head, err := runProjectGitLocal(ctx, runner, resolved, "rev-parse", "--verify", "HEAD")
	if err != nil || !projectSnapshotHeadPattern.MatchString(head) {
		return edge.OperationResult{}, errors.New("project Git HEAD is invalid")
	}
	result.GitHead = head
	branch, err := runProjectGitLocal(ctx, runner, resolved, "branch", "--show-current")
	if err != nil {
		return edge.OperationResult{}, errors.New("project Git branch is unavailable")
	}
	if branch == "" {
		result.GitDetached = true
	} else if !validProjectSnapshotBranch(branch) {
		return edge.OperationResult{}, errors.New("project Git branch is invalid")
	} else {
		result.GitBranch = branch
	}
	status, err := runProjectGitLocal(ctx, runner, resolved, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return edge.OperationResult{}, errors.New("project Git status is unavailable")
	}
	result.GitDirty = status != ""
	result.GitClean = !result.GitDirty
	if result.GitDetached {
		return result, nil
	}
	expectedRemote := "https://github.com/" + credential.Owner + "/" + resolved.Project.Repository + ".git"
	remote, err := runProjectGitLocal(ctx, runner, resolved, "remote", "get-url", "origin")
	if err != nil || remote != expectedRemote {
		return edge.OperationResult{}, errors.New("project Git remote is not owner-bound")
	}
	pushRemote, err := runProjectGitLocal(ctx, runner, resolved, "remote", "get-url", "--push", "origin")
	if err != nil || pushRemote != expectedRemote {
		return edge.OperationResult{}, errors.New("project Git push remote is not owner-bound")
	}
	upstream, err := runProjectGitLocal(ctx, runner, resolved, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || upstream != "origin/"+branch {
		return edge.OperationResult{}, errors.New("project Git upstream is invalid")
	}
	live, err := runProjectGitRemote(ctx, runner, resolved, credential, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	fields := strings.Fields(live)
	if err != nil || len(fields) != 2 || fields[1] != "refs/heads/"+branch || !projectSnapshotHeadPattern.MatchString(fields[0]) {
		return edge.OperationResult{}, errors.New("project Git remote HEAD is invalid")
	}
	result.GitRemoteHead = fields[0]
	tracked, trackErr := runProjectGitLocal(ctx, runner, resolved, "rev-parse", "--verify", "refs/remotes/origin/"+branch)
	if trackErr != nil || tracked != result.GitRemoteHead {
		return result, nil
	}
	result.GitFetched = true
	counts, err := runProjectGitLocal(ctx, runner, resolved, "rev-list", "--left-right", "--count", head+"..."+tracked)
	parts := strings.Fields(counts)
	if err != nil || len(parts) != 2 {
		return edge.OperationResult{}, errors.New("project Git relation is unavailable")
	}
	ahead, firstErr := strconv.Atoi(parts[0])
	behind, secondErr := strconv.Atoi(parts[1])
	if firstErr != nil || secondErr != nil || ahead < 0 || behind < 0 {
		return edge.OperationResult{}, errors.New("project Git relation is invalid")
	}
	result.GitAhead, result.GitBehind = ahead, behind
	result.GitDiverged = ahead > 0 && behind > 0
	return result, nil
}

func fetchProjectGitCheckout(ctx context.Context, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential) (edge.OperationResult, error) {
	before, err := inspectProjectGitCheckout(ctx, resolved, runner, credential)
	if err != nil || before.GitDetached {
		return edge.OperationResult{}, errors.New("project Git fetch preflight failed")
	}
	refspec := "refs/heads/" + before.GitBranch + ":refs/remotes/origin/" + before.GitBranch
	if _, err := runProjectGitRemote(ctx, runner, resolved, credential, "fetch", "--no-tags", "origin", refspec); err != nil {
		return edge.OperationResult{}, errors.New("project Git fetch failed")
	}
	after, err := inspectProjectGitCheckout(ctx, resolved, runner, credential)
	if err != nil || !after.GitFetched {
		return edge.OperationResult{}, errors.New("project Git fetch verification failed")
	}
	after.GitFetched = true
	return after, nil
}

func previewProjectGitFastForward(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	status, err := inspectProjectGitCheckout(ctx, resolved, runner, credential)
	if err != nil || status.GitDetached || !status.GitClean || !status.GitFetched || status.GitDiverged || status.GitAhead != 0 {
		return edge.OperationResult{}, errors.New("project Git checkout cannot fast-forward")
	}
	if _, err := runProjectGitLocal(ctx, runner, resolved, "merge-base", "--is-ancestor", status.GitHead, status.GitRemoteHead); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward relation rejected")
	}
	id, err := newProjectGitPlanID()
	if err != nil {
		return edge.OperationResult{}, err
	}
	plan := projectGitFastForwardPlan{Version: 1, ID: id, WorkspaceID: resolved.Workspace.ID, Alias: resolved.Project.Alias, Target: resolved.TargetAlias, Branch: status.GitBranch, Head: status.GitHead, RemoteHead: status.GitRemoteHead, ExpiresAt: now.UTC().Add(projectGitPlanTTL)}
	if err := writeProjectGitPlan(stateRoot, plan, true); err != nil {
		return edge.OperationResult{}, err
	}
	status.GitPlanID, status.GitPlanExpiresAt = id, plan.ExpiresAt.Format(time.RFC3339)
	return status, nil
}

func executeProjectGitFastForward(ctx context.Context, stateRoot string, resolved edgeclient.ProjectResolution, planID string, runner edgeclient.DevGitCommandRunner, credential edgeclient.GitHubCredential, now time.Time) (edge.OperationResult, error) {
	plan, err := readProjectGitPlan(stateRoot, planID)
	if err != nil || plan.Used || !plan.ExpiresAt.After(now.UTC()) || plan.WorkspaceID != resolved.Workspace.ID || plan.Alias != resolved.Project.Alias || plan.Target != resolved.TargetAlias {
		return edge.OperationResult{}, errors.New("project Git fast-forward plan is unavailable")
	}
	status, err := inspectProjectGitCheckout(ctx, resolved, runner, credential)
	if err != nil || !status.GitClean || !status.GitFetched || status.GitBranch != plan.Branch || status.GitHead != plan.Head || status.GitRemoteHead != plan.RemoteHead || status.GitDiverged || status.GitAhead != 0 {
		return edge.OperationResult{}, errors.New("project Git fast-forward state changed")
	}
	if err := consumeProjectGitPlan(stateRoot, plan.ID); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward plan was already consumed")
	}
	plan.Used = true
	if err := writeProjectGitPlan(stateRoot, plan, false); err != nil {
		return edge.OperationResult{}, err
	}
	if _, err := runProjectGitLocal(ctx, runner, resolved, "merge", "--ff-only", plan.RemoteHead); err != nil {
		return edge.OperationResult{}, errors.New("project Git fast-forward failed")
	}
	after, err := inspectProjectGitCheckout(ctx, resolved, runner, credential)
	if err != nil || after.GitHead != plan.RemoteHead || after.GitRemoteHead != plan.RemoteHead || !after.GitClean {
		return edge.OperationResult{}, errors.New("project Git fast-forward verification failed")
	}
	after.GitFastForwarded = plan.Head != plan.RemoteHead
	return after, nil
}

func projectGitMetadata(resolved edgeclient.ProjectResolution) edge.OperationResult {
	return edge.OperationResult{WorkspaceID: resolved.Workspace.ID, ProjectAlias: resolved.Project.Alias, ProjectOwner: resolved.Project.Owner, ProjectRepository: resolved.Project.Repository, ProjectTarget: resolved.TargetAlias, ProjectState: "ready", ProjectProfile: string(resolved.Workspace.Profile), ProjectMode: string(resolved.Workspace.Mode)}
}

func runProjectGitLocal(ctx context.Context, runner edgeclient.DevGitCommandRunner, resolved edgeclient.ProjectResolution, args ...string) (string, error) {
	out, err := runner.Run(ctx, resolved.Workspace.Path, args, edgeclient.GitHubCredential{})
	return strings.TrimSpace(out), err
}

func runProjectGitRemote(ctx context.Context, runner edgeclient.DevGitCommandRunner, resolved edgeclient.ProjectResolution, credential edgeclient.GitHubCredential, args ...string) (string, error) {
	out, err := runner.Run(ctx, resolved.Workspace.Path, args, credential)
	return strings.TrimSpace(out), err
}

func newProjectGitPlanID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("project Git plan generation failed")
	}
	return "gp_" + hex.EncodeToString(raw), nil
}

func projectGitPlanPath(stateRoot, id string) (string, error) {
	if len(id) != 35 || !strings.HasPrefix(id, "gp_") || strings.IndexFunc(id[3:], func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) != -1 {
		return "", errors.New("project Git plan id is invalid")
	}
	root := filepath.Join(stateRoot, "project-git-plans")
	if err := os.Mkdir(root, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !projectGitOwned(info) {
		return "", errors.New("project Git plan root is unsafe")
	}
	return filepath.Join(root, id+".json"), nil
}

func writeProjectGitPlan(stateRoot string, plan projectGitFastForwardPlan, create bool) error {
	path, err := projectGitPlanPath(stateRoot, plan.ID)
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
		_, w := f.Write(append(content, '\n'))
		s := f.Sync()
		c := f.Close()
		if w != nil || s != nil || c != nil {
			return errors.New("project Git plan write failed")
		}
		return nil
	}
	temp := path + ".tmp"
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, w := f.Write(append(content, '\n'))
	s := f.Sync()
	c := f.Close()
	if w != nil || s != nil || c != nil {
		_ = os.Remove(temp)
		return errors.New("project Git plan update failed")
	}
	return os.Rename(temp, path)
}

func readProjectGitPlan(stateRoot, id string) (projectGitFastForwardPlan, error) {
	path, err := projectGitPlanPath(stateRoot, id)
	if err != nil {
		return projectGitFastForwardPlan{}, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || info.Size() > 4096 || !projectGitOwned(info) {
		return projectGitFastForwardPlan{}, errors.New("project Git plan is unsafe")
	}
	f, err := os.Open(path)
	if err != nil {
		return projectGitFastForwardPlan{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 4096))
	dec.DisallowUnknownFields()
	var plan projectGitFastForwardPlan
	if dec.Decode(&plan) != nil || dec.Decode(&struct{}{}) != io.EOF || plan.Version != 1 || plan.ID != id || plan.ExpiresAt.IsZero() ||
		!projectSnapshotHeadPattern.MatchString(plan.Head) || !projectSnapshotHeadPattern.MatchString(plan.RemoteHead) || !validProjectSnapshotBranch(plan.Branch) ||
		plan.WorkspaceID == "" || plan.Alias == "" || plan.Target == "" {
		return projectGitFastForwardPlan{}, errors.New("project Git plan is invalid")
	}
	return plan, nil
}

func consumeProjectGitPlan(stateRoot, id string) error {
	path, err := projectGitPlanPath(stateRoot, id)
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
	return file.Close()
}

func projectGitOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
