package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

type repositoryStatus struct {
	Repository string
	Dir        string
	Branch     string
	Head       string
	Upstream   string
	Ahead      int
	Behind     int
	Staged     []string
	Modified   []string
	Untracked  []string
	Clean      bool
	Detached   bool
}

// RepoStatus returns a stable, argument-free view of repository synchronization
// and working-tree state. It never accepts arbitrary git arguments.
func (s *GitCapability) RepoStatus(repo string) (string, error) {
	sp := s.log.Start("repo_status")
	status, err := s.readRepositoryStatus(repo)
	if err != nil {
		sp.Finish(audit.Error, "repo_status", nil, err)
		return "", err
	}
	sp.Finish(audit.Allow, "repo_status", []string{status.Dir}, nil)
	return formatRepositoryStatus(status), nil
}

func (s *GitCapability) readRepositoryStatus(repo string) (repositoryStatus, error) {
	dir, err := s.workdir(repo)
	if err != nil {
		return repositoryStatus{}, err
	}
	args := []string{"status", "--porcelain=v2", "--branch"}
	out, err := s.gitRead(dir, args...)
	if err != nil {
		return repositoryStatus{}, fmt.Errorf("git status: %w", err)
	}
	st := repositoryStatus{Dir: dir, Clean: true}
	if rel, relErr := filepath.Rel(s.root, dir); relErr == nil && rel != "." {
		st.Repository = filepath.ToSlash(rel)
	} else {
		st.Repository = filepath.Base(dir)
	}
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			st.Head = strings.TrimPrefix(line, "# branch.oid ")
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			st.Detached = st.Branch == "(detached)"
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		case strings.HasPrefix(line, "# branch.ab "):
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &st.Ahead, &st.Behind)
		case strings.HasPrefix(line, "? "):
			st.Untracked = append(st.Untracked, strings.TrimPrefix(line, "? "))
			st.Clean = false
		case strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") || strings.HasPrefix(line, "u "):
			limit := 9
			if strings.HasPrefix(line, "2 ") {
				limit = 10
			} else if strings.HasPrefix(line, "u ") {
				limit = 11
			}
			fields := strings.SplitN(line, " ", limit)
			if len(fields) < 3 {
				continue
			}
			xy := fields[1]
			path := strings.SplitN(fields[len(fields)-1], "\t", 2)[0]
			if unquoted, err := strconv.Unquote(path); err == nil {
				path = unquoted
			}
			if len(xy) == 2 && xy[0] != '.' {
				st.Staged = append(st.Staged, path)
			}
			if len(xy) == 2 && xy[1] != '.' {
				st.Modified = append(st.Modified, path)
			}
			st.Clean = false
		}
	}
	return st, nil
}

func formatRepositoryStatus(st repositoryStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "repository: %s\npath: %s\nbranch: %s\nhead: %s\nupstream: %s\nahead: %d\nbehind: %d\n", st.Repository, st.Dir, st.Branch, st.Head, st.Upstream, st.Ahead, st.Behind)
	writeStatusFiles(&b, "staged_files", st.Staged)
	writeStatusFiles(&b, "modified_files", st.Modified)
	writeStatusFiles(&b, "untracked_files", st.Untracked)
	fmt.Fprintf(&b, "clean: %t\ndetached_head: %t\n", st.Clean, st.Detached)
	return b.String()
}

func writeStatusFiles(b *strings.Builder, label string, files []string) {
	fmt.Fprintf(b, "%s:", label)
	if len(files) == 0 {
		b.WriteString(" []\n")
		return
	}
	b.WriteByte('\n')
	for _, file := range files {
		fmt.Fprintf(b, "- %s\n", file)
	}
}

// RepoFetch runs exactly `git fetch <remote>` after jail, name, policy, approval,
// and audit checks. Refspecs and extra arguments are not representable.
func (s *Service) RepoFetch(repo, remote string, approve bool) (string, error) {
	sp := s.log.Start("repo_fetch")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "repo_fetch", nil, err)
		return "", err
	}
	remote = defaultGitName(remote, "origin")
	if !safeGitName(remote) || strings.Contains(remote, "/") {
		err := fmt.Errorf("invalid git remote %q", remote)
		sp.Finish(audit.Deny, "repo_fetch", []string{dir}, err)
		return "", err
	}
	args := []string{"fetch", remote}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		sp.Finish(audit.Deny, summarize(args...), []string{dir}, err)
		return "", err
	}
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, summarize(args...), []string{dir}, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, summarize(args...), []string{dir}, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: repo_fetch would run git fetch %s in %s. Re-invoke with approve=true.", remote, filepath.Base(dir)), nil
	}
	out, runErr := s.run(context.Background(), dir, "git", args)
	if runErr != nil {
		sp.Finish(audit.Error, summarize(args...), []string{dir}, runErr)
		return s.redact(out), fmt.Errorf("git fetch: %w", runErr)
	}
	sp.Finish(audit.Allow, summarize(args...), []string{dir}, nil)
	return s.redact(out), nil
}

func (s *Service) RepoFastForwardPreview(repo string) (string, error) {
	sp := s.log.Start("repo_fast_forward_preview")
	st, err := s.readRepositoryStatus(repo)
	if err != nil {
		sp.Finish(audit.Error, "preview", nil, err)
		return "", err
	}
	if st.Detached || st.Branch == "" || st.Branch == "(initial)" {
		err := fmt.Errorf("fast-forward requires an attached branch; detached HEAD is rejected")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	if !st.Clean {
		err := fmt.Errorf("fast-forward preview requires a clean working tree")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	if st.Upstream == "" || !safeGitName(st.Upstream) {
		err := fmt.Errorf("current branch has no safe upstream")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	target, err := s.gitRead(st.Dir, "rev-parse", st.Upstream)
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
		return "", fmt.Errorf("resolving upstream target: %w", err)
	}
	target = strings.TrimSpace(target)
	valid := true
	if _, err := s.gitRead(st.Dir, "merge-base", "--is-ancestor", st.Head, target); err != nil {
		valid = false
	}
	if !valid {
		err := fmt.Errorf("upstream is not a fast-forward of current HEAD; divergence rejected")
		sp.Finish(audit.Deny, "preview", []string{st.Dir}, err)
		return "", err
	}
	commits, err := s.gitRead(st.Dir, "log", "--format=%H %s", st.Head+".."+target)
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
		return "", err
	}
	plan, err := s.plans.Create("repo-fast-forward", map[string]string{
		"repo": st.Dir, "branch": st.Branch, "head": st.Head, "upstream": st.Upstream, "target": target,
	})
	if err != nil {
		sp.Finish(audit.Error, "preview", []string{st.Dir}, err)
		return "", err
	}
	sp.Finish(audit.Allow, "preview "+plan.ID, []string{st.Dir}, nil)
	return fmt.Sprintf("repo: %s\ncurrent_branch: %s\nupstream: %s\ncurrent_head: %s\ntarget_sha: %s\nclean: true\nvalid_fast_forward: true\ncommits_to_add:\n%s\ncommand: git merge --ff-only %s\nplan_id: %s\nexpiry: %s\n",
		st.Repository, st.Branch, st.Upstream, st.Head, target, strings.TrimSpace(commits), st.Upstream, plan.ID, plan.ExpiresAt.Format(time.RFC3339)), nil
}

func (s *Service) RepoFastForward(planID string, approve bool) (string, error) {
	sp := s.log.Start("repo_fast_forward")
	needsApproval, err := s.pol.CheckAction()
	if err != nil {
		sp.Finish(audit.Deny, planID, nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, planID, nil, nil)
		return "APPROVAL REQUIRED: repo_fast_forward would execute the reviewed single-use plan. Re-invoke with approve=true.", nil
	}
	plan, err := s.plans.Consume(strings.TrimSpace(planID), "repo-fast-forward")
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
		err := fmt.Errorf("branch changed after preview")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	if st.Head != plan.Args["head"] {
		err := fmt.Errorf("HEAD changed after preview")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	target, err := s.gitRead(st.Dir, "rev-parse", plan.Args["upstream"])
	if err != nil || strings.TrimSpace(target) != plan.Args["target"] {
		if err == nil {
			err = fmt.Errorf("target changed after preview")
		}
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	if _, err := s.gitRead(st.Dir, "merge-base", "--is-ancestor", st.Head, plan.Args["target"]); err != nil {
		err := fmt.Errorf("fast-forward relationship changed; divergence rejected")
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	args := []string{"merge", "--ff-only", plan.Args["upstream"]}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		sp.Finish(audit.Deny, planID, []string{st.Dir}, err)
		return "", err
	}
	out, runErr := s.run(context.Background(), st.Dir, "git", args)
	if runErr != nil {
		sp.Finish(audit.Error, planID, []string{st.Dir}, runErr)
		return s.redact(out), fmt.Errorf("git merge --ff-only: %w", runErr)
	}
	sp.Finish(audit.Allow, planID, []string{st.Dir}, nil)
	return s.redact(out), nil
}

func (s *GitCapability) gitRead(dir string, args ...string) (string, error) {
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		return "", err
	}
	out, err := s.run(context.Background(), dir, "git", args)
	if err != nil {
		return s.redact(out), err
	}
	return s.redact(out), nil
}

func parseCount(v string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}
