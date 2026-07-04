package tools

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

var gitSafeNameRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
var gitSafeDirRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// GitClone clones a repository into a new directory directly under the service
// root. It is controlled command execution: no shell, no embedded credentials, no
// target escapes, mode-gated, audited, and output-redacted.
func (s *Service) GitClone(remoteURL, dir string, approve bool) (string, error) {
	sp := s.log.Start("git_clone")
	remoteURL = strings.TrimSpace(remoteURL)
	if err := validateCloneURL(remoteURL); err != nil {
		sp.Finish(audit.Deny, "git_clone", nil, err)
		return "", err
	}
	target := strings.TrimSpace(dir)
	if target == "" {
		var err error
		target, err = cloneDirFromURL(remoteURL)
		if err != nil {
			sp.Finish(audit.Deny, "git_clone", nil, err)
			return "", err
		}
	}
	if !safeCloneDir(target) {
		err := fmt.Errorf("invalid clone target %q (use a simple directory name)", dir)
		sp.Finish(audit.Deny, "git_clone", nil, err)
		return "", err
	}
	resolved, needsApproval, err := s.pol.CheckWrite(filepath.Join(s.root, target))
	if err != nil {
		sp.Finish(audit.Deny, "git_clone "+target, nil, err)
		return "", err
	}
	if _, err := os.Stat(resolved); err == nil {
		err := fmt.Errorf("clone target already exists: %s", target)
		sp.Finish(audit.Deny, "git_clone "+target, []string{resolved}, err)
		return "", err
	} else if !os.IsNotExist(err) {
		sp.Finish(audit.Error, "git_clone "+target, []string{resolved}, err)
		return "", err
	}
	args := []string{"clone", remoteURL, target}
	if cmdApproval, err := s.pol.CheckCommand("git", args); err != nil {
		sp.Finish(audit.Deny, "git_clone "+target, nil, err)
		return "", err
	} else {
		needsApproval = needsApproval || cmdApproval
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "git_clone "+target, []string{resolved}, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: git_clone would clone into %s. Re-invoke with approve=true.", target), nil
	}
	out, runErr := s.run(context.Background(), s.root, "git", args)
	if runErr != nil {
		sp.Finish(audit.Error, "git_clone "+target, []string{resolved}, runErr)
		return s.redact(out), fmt.Errorf("git clone: %w", runErr)
	}
	sp.Finish(audit.Allow, "git_clone "+target, []string{resolved}, nil)
	return s.redact(out), nil
}

// GitPush pushes one branch to one named remote from a selected repo. It does not
// accept extra git args, so force pushes, tag pushes, and URL remotes are not
// expressible through this tool.
func (s *Service) GitPush(repo, remote, branch string, approve bool) (string, error) {
	sp := s.log.Start("git_push")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "git_push", nil, err)
		return "", err
	}
	remote = defaultGitName(remote, "origin")
	if !safeGitName(remote) {
		err := fmt.Errorf("invalid git remote %q (use a remote name, not a URL or option)", remote)
		sp.Finish(audit.Deny, "git_push", []string{dir}, err)
		return "", err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		if err := s.pol.CheckCommandAllowed("git", []string{"branch", "--show-current"}); err != nil {
			sp.Finish(audit.Deny, "git branch --show-current", []string{dir}, err)
			return "", err
		}
		out, err := s.run(context.Background(), dir, "git", []string{"branch", "--show-current"})
		if err != nil {
			sp.Finish(audit.Error, "git branch --show-current", []string{dir}, err)
			return s.redact(out), fmt.Errorf("git branch: %w", err)
		}
		branch = strings.TrimSpace(out)
		if branch == "" {
			err := fmt.Errorf("cannot infer current branch; pass branch explicitly")
			sp.Finish(audit.Error, "git_push", []string{dir}, err)
			return "", err
		}
	}
	if !safeGitName(branch) {
		err := fmt.Errorf("invalid git branch %q", branch)
		sp.Finish(audit.Deny, "git_push", []string{dir}, err)
		return "", err
	}
	if err := s.pol.CheckCommandAllowed("git", []string{"branch", "--show-current"}); err != nil {
		sp.Finish(audit.Deny, "git_push", []string{dir}, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, "git_push", []string{dir}, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "git_push "+remote+" "+branch, []string{dir}, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: git_push would push %s to %s from %s. Re-invoke with approve=true.", branch, remote, filepath.Base(dir)), nil
	}
	args := []string{"push", remote, branch}
	out, runErr := s.run(context.Background(), dir, "git", args)
	if runErr != nil {
		sp.Finish(audit.Error, "git_push "+remote+" "+branch, []string{dir}, runErr)
		return s.redact(out), fmt.Errorf("git push: %w", runErr)
	}
	sp.Finish(audit.Allow, "git_push "+remote+" "+branch, []string{dir}, nil)
	return s.redact(out), nil
}

func validateCloneURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("remote URL is required")
	}
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid remote URL")
	}
	if u.User != nil {
		return fmt.Errorf("remote URL must not include embedded credentials")
	}
	switch u.Scheme {
	case "https", "http", "ssh", "git":
		return nil
	default:
		return fmt.Errorf("unsupported remote URL scheme %q", u.Scheme)
	}
}

func cloneDirFromURL(raw string) (string, error) {
	raw = strings.TrimSuffix(raw, "/")
	last := raw
	if i := strings.LastIndexAny(raw, "/:"); i >= 0 {
		last = raw[i+1:]
	}
	last = strings.TrimSuffix(last, ".git")
	if !safeCloneDir(last) {
		return "", fmt.Errorf("could not infer a safe clone directory from URL")
	}
	return last, nil
}

func safeCloneDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	return dir != "" && dir != "." && dir != ".." && !strings.HasPrefix(dir, "-") &&
		!strings.ContainsAny(dir, `/\`) && gitSafeDirRe.MatchString(dir)
}

func defaultGitName(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

func safeGitName(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && v != "." && v != ".." && !strings.HasPrefix(v, "-") &&
		!strings.Contains(v, "..") && gitSafeNameRe.MatchString(v)
}
