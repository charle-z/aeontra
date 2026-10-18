package tools

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

func (s *GitCapability) RepoRemotePreview(repo, remote, repository string) (string, error) {
	sp := s.log.Start("repo_remote_preview")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "preview", nil, err)
		return "", err
	}
	remote = defaultGitName(remote, "origin")
	if !safeGitName(remote) || strings.Contains(remote, "/") {
		err := fmt.Errorf("invalid git remote %q", remote)
		sp.Finish(audit.Deny, "preview", []string{dir}, err)
		return "", err
	}
	proposed := strings.TrimSpace(repository)
	if safeCloneDir(proposed) {
		proposed = "https://github.com/" + s.github.owner + "/" + proposed + ".git"
	}
	proposed, err = sanitizeGitHubRemoteURL(proposed, s.github.owner)
	if err != nil {
		sp.Finish(audit.Deny, "preview", []string{dir}, err)
		return "", err
	}
	current, exists, err := s.currentRemoteURL(dir, remote)
	if err != nil {
		sp.Finish(audit.Deny, "preview", []string{dir}, err)
		return "", err
	}
	action := "add"
	command := fmt.Sprintf("git remote add %s %s", remote, proposed)
	if exists {
		action = "update"
		command = fmt.Sprintf("git remote set-url %s %s", remote, proposed)
	}
	plan, err := s.plans.Create("repo-remote-set", map[string]string{
		"repo": dir, "remote": remote, "current": current, "exists": fmt.Sprintf("%t", exists), "proposed": proposed, "action": action,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{dir}, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, []string{dir}, nil)
	return fmt.Sprintf("repo: %s\nremote: %s\ncurrent_url: %s\nproposed_url: %s\naction: %s\ncommand: %s\nplan_id: %s\nexpiry: %s\n",
		filepath.Base(dir), remote, current, proposed, action, command, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *GitCapability) RepoRemoteSet(planID string, approve bool) (string, error) {
	sp := s.log.Start("repo_remote_set")
	if err := s.github.configError(); err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: repo_remote_set would execute the reviewed single-use remote plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "repo-remote-set")
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	dir, err := s.workdir(plan.Args["repo"])
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	current, exists, err := s.currentRemoteURL(dir, plan.Args["remote"])
	if err != nil {
		sp.Finish(audit.Deny, planID, []string{dir}, err)
		return "", err
	}
	wantExists := plan.Args["exists"] == "true"
	if exists != wantExists || current != plan.Args["current"] {
		err := fmt.Errorf("remote changed after preview")
		sp.Finish(audit.Deny, planID, []string{dir}, err)
		return "", err
	}
	proposed, err := sanitizeGitHubRemoteURL(plan.Args["proposed"], s.github.owner)
	if err != nil {
		sp.Finish(audit.Deny, planID, []string{dir}, err)
		return "", err
	}
	args := []string{"remote", "add", plan.Args["remote"], proposed}
	if wantExists {
		args = []string{"remote", "set-url", plan.Args["remote"], proposed}
	}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		sp.Finish(audit.Deny, planID, []string{dir}, err)
		return "", err
	}
	out, runErr := s.run(context.Background(), dir, "git", args)
	if runErr != nil {
		sp.Finish(audit.Error, planID, []string{dir}, runErr)
		return s.redact(out), fmt.Errorf("setting git remote: %w", runErr)
	}
	sp.Finish(audit.Allow, planID, []string{dir}, nil)
	return fmt.Sprintf("remote %s %s: %s", plan.Args["remote"], plan.Args["action"], proposed), nil
}

func (s *GitCapability) currentRemoteURL(dir, remote string) (string, bool, error) {
	args := []string{"remote", "get-url", remote}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		return "", false, err
	}
	out, err := s.run(context.Background(), dir, "git", args)
	if err != nil {
		return "", false, nil
	}
	raw := strings.TrimSpace(out)
	if raw == "" {
		return "", false, nil
	}
	clean, err := sanitizeGitHubRemoteURL(raw, s.github.owner)
	if err != nil {
		return "", false, fmt.Errorf("existing remote URL is unsafe: %w", err)
	}
	return clean, true, nil
}

func sanitizeGitHubRemoteURL(raw, owner string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("repository name or URL is required")
	}
	var remoteOwner string
	switch {
	case strings.HasPrefix(raw, "git@github.com:"):
		path := strings.TrimPrefix(raw, "git@github.com:")
		parts := strings.Split(strings.TrimSuffix(path, ".git"), "/")
		if len(parts) != 2 || !safeCloneDir(parts[0]) || !safeCloneDir(parts[1]) {
			return "", fmt.Errorf("invalid GitHub SSH remote")
		}
		remoteOwner = parts[0]
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") {
			return "", fmt.Errorf("remote host must be github.com")
		}
		if u.User != nil {
			if u.Scheme != "ssh" || u.User.Username() != "git" || strings.Contains(raw, "git:") {
				return "", fmt.Errorf("remote URL must not contain credentials")
			}
		}
		if u.Scheme != "https" && u.Scheme != "ssh" {
			return "", fmt.Errorf("remote URL must use HTTPS or supported SSH")
		}
		parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
		if len(parts) != 2 || !safeCloneDir(parts[0]) || !safeCloneDir(parts[1]) {
			return "", fmt.Errorf("invalid GitHub repository path")
		}
		remoteOwner = parts[0]
	default:
		return "", fmt.Errorf("repository must be a name or credential-free GitHub URL")
	}
	if !strings.EqualFold(remoteOwner, owner) {
		return "", fmt.Errorf("remote owner %q is not configured GITHUB_OWNER %q", remoteOwner, owner)
	}
	return raw, nil
}

func sanitizeCredentialFreeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("URL contains credentials")
	}
	return u.String(), nil
}
