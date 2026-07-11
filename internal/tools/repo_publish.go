package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

type remoteBranchState struct {
	Exists bool
	SHA    string
}

var gitObjectIDRe = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// RepoPublishPreview validates one current local branch against one named,
// owner-restricted remote. No push URL, refspec, force, mirror, or tag option is
// accepted from the caller.
func (s *Service) RepoPublishPreview(repo, remote, branch string) (string, error) {
	sp := s.log.Start("repo_publish_preview")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	st, err := s.readRepositoryStatus(repo)
	if err != nil {
		sp.Finish(audit.Error, "preview", nil, err)
		return "", err
	}
	if st.Detached || st.Branch == "" || st.Branch == "(initial)" {
		err := fmt.Errorf("publication requires an attached branch; detached HEAD is rejected")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	if !st.Clean {
		err := fmt.Errorf("publication requires a clean working tree")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	remote = defaultGitName(remote, "origin")
	if !safeGitName(remote) || strings.Contains(remote, "/") {
		err := fmt.Errorf("invalid git remote %q", remote)
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	branch = defaultGitName(branch, st.Branch)
	if !safeGitName(branch) || strings.Contains(branch, "..") {
		err := fmt.Errorf("invalid git branch %q", branch)
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	if branch != st.Branch {
		err := fmt.Errorf("repo_publish publishes only the currently checked-out branch (%s)", st.Branch)
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	remoteURL, exists, err := s.currentRemoteURL(st.Dir, remote)
	if err != nil {
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	if !exists {
		err := fmt.Errorf("git remote %q does not exist", remote)
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	remoteState, err := s.readRemoteBranch(st.Dir, remoteURL, remote, branch)
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
		return "", err
	}
	ahead, behind := 0, 0
	command := fmt.Sprintf("git push -u %s %s", remote, branch)
	if remoteState.Exists {
		if _, err := s.gitRead(st.Dir, "cat-file", "-e", remoteState.SHA+"^{commit}"); err != nil {
			err := fmt.Errorf("remote branch commit is not available locally; run repo_fetch before publication preview")
			sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
			return "", err
		}
		counts, err := s.gitRead(st.Dir, "rev-list", "--left-right", "--count", remoteState.SHA+"..."+st.Head)
		if err != nil {
			sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
			return "", err
		}
		parts := strings.Fields(counts)
		if len(parts) != 2 {
			err := fmt.Errorf("unexpected git rev-list count output")
			sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
			return "", err
		}
		behind, ahead = parseCount(parts[0]), parseCount(parts[1])
		if behind > 0 {
			err := fmt.Errorf("local branch is behind remote by %d commit(s); divergence or non-fast-forward publication rejected", behind)
			sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
			return "", err
		}
		command = fmt.Sprintf("git push %s %s", remote, branch)
	} else {
		count, err := s.gitRead(st.Dir, "rev-list", "--count", st.Head)
		if err != nil {
			sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
			return "", err
		}
		ahead = parseCount(count)
	}
	plan, err := s.plans.Create("repo-publish", map[string]string{
		"repo": st.Dir, "branch": branch, "head": st.Head, "remote": remote,
		"remote_url": remoteURL, "remote_exists": fmt.Sprintf("%t", remoteState.Exists),
		"remote_sha": remoteState.SHA, "command": command,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, []string{st.Dir}, nil)
	return fmt.Sprintf("repo: %s\nbranch: %s\nlocal_head: %s\nremote: %s\nsanitized_url: %s\nremote_branch_exists: %t\nahead: %d\nbehind: %d\ncommand: %s\nplan_id: %s\nexpiry: %s\n",
		filepath.Base(st.Dir), branch, st.Head, remote, remoteURL, remoteState.Exists, ahead, behind, command, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) RepoPublish(planID string, approve bool) (string, error) {
	sp := s.log.Start("repo_publish")
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: repo_publish would execute the reviewed single-use publication plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "repo-publish")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	st, err := s.readRepositoryStatus(plan.Args["repo"])
	if err != nil {
		sp.Finish(audit.Error, planID, nil, err)
		return "", err
	}
	if !st.Clean {
		err := fmt.Errorf("working tree changed and is no longer clean")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	if st.Detached || st.Branch != plan.Args["branch"] {
		err := fmt.Errorf("branch changed after publication preview")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	if st.Head != plan.Args["head"] {
		err := fmt.Errorf("HEAD changed after publication preview")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	remoteURL, exists, err := s.currentRemoteURL(st.Dir, plan.Args["remote"])
	if err != nil || !exists || remoteURL != plan.Args["remote_url"] {
		if err == nil {
			err = fmt.Errorf("remote changed after publication preview")
		}
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	remoteState, err := s.readRemoteBranch(st.Dir, remoteURL, plan.Args["remote"], plan.Args["branch"])
	if err != nil {
		sp.Finish(audit.Error, planID, []string{st.Dir}, err)
		return "", err
	}
	wantRemoteExists := plan.Args["remote_exists"] == "true"
	if remoteState.Exists != wantRemoteExists || remoteState.SHA != plan.Args["remote_sha"] {
		err := fmt.Errorf("remote branch changed after publication preview")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	args := []string{"push", plan.Args["remote"], plan.Args["branch"]}
	if !wantRemoteExists {
		args = []string{"push", "-u", plan.Args["remote"], plan.Args["branch"]}
	}
	// Deliberately do not route this through the generic command allowlist: generic
	// git push is always blocked there. This exact argv is generated and validated
	// by the publication plan and still passes the central action posture above.
	out, runErr := s.runGitHubRemote(context.Background(), st.Dir, remoteURL, args)
	if runErr != nil {
		sp.Finish(audit.Error, planID, []string{st.Dir}, runErr)
		return s.redact(out), fmt.Errorf("git push: %w", runErr)
	}
	sp.Finish(audit.Allow, planID, []string{st.Dir}, nil)
	return s.redact(out), nil
}

func (s *Service) readRemoteBranch(dir, remoteURL, remote, branch string) (remoteBranchState, error) {
	if !safeGitName(remote) || strings.Contains(remote, "/") || !safeGitName(branch) {
		return remoteBranchState{}, fmt.Errorf("unsafe remote or branch")
	}
	args := []string{"ls-remote", "--heads", remote, "refs/heads/" + branch}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		return remoteBranchState{}, err
	}
	out, err := s.runGitHubRemote(context.Background(), dir, remoteURL, args)
	if err != nil {
		return remoteBranchState{}, fmt.Errorf("git ls-remote: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return remoteBranchState{}, nil
	}
	if len(fields) != 2 || fields[1] != "refs/heads/"+branch {
		return remoteBranchState{}, fmt.Errorf("unexpected git ls-remote response")
	}
	if !gitObjectIDRe.MatchString(fields[0]) {
		return remoteBranchState{}, fmt.Errorf("git ls-remote returned an invalid object id")
	}
	return remoteBranchState{Exists: true, SHA: fields[0]}, nil
}
