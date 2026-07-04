package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charle-z/mcp-devbox/internal/audit"
)

// GitStatus returns the working-tree status (read-only; works in any mode). When
// repo is provided, it is resolved as a working directory inside the jail.
func (s *Service) GitStatus(repo ...string) (string, error) {
	sp := s.log.Start("git_status")
	dirArg := ""
	if len(repo) > 0 {
		dirArg = repo[0]
	}
	dir, err := s.workdir(dirArg)
	if err != nil {
		sp.Finish(audit.Deny, "git status", nil, err)
		return "", err
	}
	args := []string{"status", "--short", "--branch"}
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		sp.Finish(audit.Deny, "git status", nil, err)
		return "", err
	}
	out, err := s.run(context.Background(), dir, "git", args)
	if err != nil {
		sp.Finish(audit.Error, "git status", []string{dir}, err)
		return s.redact(out), fmt.Errorf("git status: %w", err)
	}
	sp.Finish(audit.Allow, "git status", []string{dir}, nil)
	return s.redact(out), nil
}

// GitDiff returns a diff (read-only). Optional extra args (e.g. "--staged" or a
// pathspec) are validated by the allowlist gate (no metacharacters/injection).
func (s *Service) GitDiff(extra ...string) (string, error) {
	return s.GitDiffIn("", extra...)
}

// GitDiffIn is GitDiff with an explicit, jailed repo working directory.
func (s *Service) GitDiffIn(repo string, extra ...string) (string, error) {
	sp := s.log.Start("git_diff")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "git diff", nil, err)
		return "", err
	}
	args := append([]string{"diff"}, extra...)
	if err := s.pol.CheckCommandAllowed("git", args); err != nil {
		sp.Finish(audit.Deny, summarize(args...), nil, err)
		return "", err
	}
	out, err := s.run(context.Background(), dir, "git", args)
	if err != nil {
		sp.Finish(audit.Error, summarize(args...), []string{dir}, err)
		return s.redact(out), fmt.Errorf("git diff: %w", err)
	}
	sp.Finish(audit.Allow, summarize(args...), []string{dir}, nil)
	return s.redact(out), nil
}

// GitCommit stages all changes and commits them. It is a write action: gated by the
// write/command posture (read-only denies, ask requires approve=true, allow commits).
// The message is passed via argv (never a shell), so normal punctuation is safe.
func (s *Service) GitCommit(message string, approve bool) (string, error) {
	return s.GitCommitIn("", message, approve)
}

// GitCommitIn stages and commits changes in an optional selected repo/workdir
// inside the jail. The root-level GitCommit behavior is preserved when repo is empty.
func (s *Service) GitCommitIn(repo, message string, approve bool) (string, error) {
	sp := s.log.Start("git_commit")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "git_commit", nil, err)
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		err := fmt.Errorf("commit message is required")
		sp.Finish(audit.Error, "git_commit", nil, err)
		return "", err
	}
	// Mode + allowlist gate (commit is not in the destructive git set).
	needsApproval, err := s.pol.CheckCommand("git", []string{"commit"})
	if err != nil {
		sp.Finish(audit.Deny, "git_commit", nil, err)
		return "", err
	}
	if needsApproval && !approve {
		sp.Finish(audit.Ask, "git_commit", nil, nil)
		return "APPROVAL REQUIRED: git_commit would stage all changes and commit. Re-invoke with approve=true.", nil
	}
	ctx := context.Background()
	if out, err := s.run(ctx, dir, "git", []string{"add", "-A"}); err != nil {
		sp.Finish(audit.Error, "git add -A", []string{dir}, err)
		return s.redact(out), fmt.Errorf("git add: %w", err)
	}
	out, err := s.run(ctx, dir, "git", []string{"commit", "-m", message})
	if err != nil {
		sp.Finish(audit.Error, "git commit", []string{dir}, err)
		return s.redact(out), fmt.Errorf("git commit: %w", err)
	}
	sp.Finish(audit.Allow, "git commit", []string{dir}, nil)
	return s.redact(out), nil
}

// ApplyPatch applies a unified diff, patch-first and validated. It (1) extracts the
// patch's target files and policy-checks each as a write (jail + secret + mode),
// (2) validates with `git apply --check` BEFORE applying, (3) in ask mode returns
// "approval required" without applying unless approve is true.
func (s *Service) ApplyPatch(patch string, approve bool) (string, error) {
	return s.ApplyPatchIn("", patch, approve)
}

// ApplyPatchIn applies a patch relative to an optional selected repo/workdir
// inside the jail.
func (s *Service) ApplyPatchIn(repo, patch string, approve bool) (string, error) {
	sp := s.log.Start("apply_patch")
	dir, err := s.workdir(repo)
	if err != nil {
		sp.Finish(audit.Deny, "apply_patch", nil, err)
		return "", err
	}

	targets := parsePatchTargets(patch)
	if len(targets) == 0 {
		err := fmt.Errorf("no file targets found in patch")
		sp.Finish(audit.Error, "apply_patch", nil, err)
		return "", err
	}

	var resolvedTargets []string
	needsApproval := false
	for _, tgt := range targets {
		resolved, approval, err := s.pol.CheckWrite(filepath.Join(dir, tgt))
		if err != nil {
			sp.Finish(audit.Deny, summarize(targets...), nil, err)
			return "", fmt.Errorf("patch target %q: %w", tgt, err)
		}
		resolvedTargets = append(resolvedTargets, resolved)
		needsApproval = needsApproval || approval
	}

	// Write the patch to an OS temp file (outside the repo) and validate.
	pf, err := os.CreateTemp("", "mcp-devbox-*.patch")
	if err != nil {
		sp.Finish(audit.Error, "apply_patch", resolvedTargets, err)
		return "", err
	}
	defer os.Remove(pf.Name())
	if _, err := pf.WriteString(patch); err != nil {
		pf.Close()
		sp.Finish(audit.Error, "apply_patch", resolvedTargets, err)
		return "", err
	}
	pf.Close()

	checkArgs := []string{"apply", "--check", "--verbose", pf.Name()}
	if out, err := s.run(context.Background(), dir, "git", checkArgs); err != nil {
		sp.Finish(audit.Error, summarize(targets...), resolvedTargets, err)
		return "", fmt.Errorf("patch failed validation (git apply --check):\n%s", s.redact(out))
	}

	if needsApproval && !approve {
		sp.Finish(audit.Ask, summarize(targets...), resolvedTargets, nil)
		return fmt.Sprintf("APPROVAL REQUIRED: patch validated cleanly and affects %d file(s): %s\nRe-invoke apply_patch with approve=true to apply.",
			len(targets), strings.Join(targets, ", ")), nil
	}

	applyArgs := []string{"apply", pf.Name()}
	if out, err := s.run(context.Background(), dir, "git", applyArgs); err != nil {
		sp.Finish(audit.Error, summarize(targets...), resolvedTargets, err)
		return "", fmt.Errorf("git apply failed:\n%s", s.redact(out))
	}
	sp.Finish(audit.Allow, summarize(targets...), resolvedTargets, nil)
	return fmt.Sprintf("Applied patch to %d file(s): %s", len(targets), strings.Join(targets, ", ")), nil
}

// parsePatchTargets extracts target file paths from a unified diff. It reads the
// "+++ b/<path>" and "--- a/<path>" headers plus rename/copy targets, stripping the
// a//b/ prefixes. Paths are returned as-is (relative); the caller jails them.
func parsePatchTargets(patch string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || p == "/dev/null" {
			return
		}
		p = stripABPrefix(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			add(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "--- "):
			add(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "rename to "):
			add(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "rename from "):
			add(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "copy to "):
			add(strings.TrimPrefix(line, "copy to "))
		}
	}
	return out
}

// stripABPrefix removes a leading "a/" or "b/" (and surrounding quotes/tabs) from a
// diff path. A trailing tab+timestamp (some diff tools add it) is also dropped.
func stripABPrefix(p string) string {
	p = strings.Trim(p, "\"")
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		p = p[2:]
	}
	return p
}
