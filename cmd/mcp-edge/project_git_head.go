package main

import (
	"context"
	"errors"
	"strings"
)

// observeProjectHead distinguishes an attached branch with no first commit
// from a damaged HEAD. A missing branch ref is accepted only after Git confirms
// the symbolic HEAD and returns its documented not-found exit status.
func observeProjectHead(ctx context.Context, run func(...string) (string, error), validCommit, validBranch func(string) bool) (head, branch string, unborn, detached bool, err error) {
	headOutput, headErr := run("rev-parse", "--verify", "HEAD")
	branchOutput, branchErr := run("branch", "--show-current")
	if branchErr != nil || ctx.Err() != nil {
		return "", "", false, false, errors.New("project Git branch is unavailable")
	}
	branch = strings.TrimSpace(branchOutput)
	if branch == "" {
		if headErr != nil || !validCommit(strings.TrimSpace(headOutput)) {
			return "", "", false, false, errors.New("detached project Git HEAD is invalid")
		}
		return strings.TrimSpace(headOutput), "", false, true, nil
	}
	if !validBranch(branch) {
		return "", "", false, false, errors.New("project Git branch is invalid")
	}
	if headErr == nil {
		head = strings.TrimSpace(headOutput)
		if !validCommit(head) {
			return "", "", false, false, errors.New("project Git HEAD is invalid")
		}
		return head, branch, false, false, nil
	}
	if ctx.Err() != nil {
		return "", "", false, false, errors.New("project Git HEAD inspection timed out")
	}
	symbolic, symbolicErr := run("symbolic-ref", "--quiet", "--short", "HEAD")
	if symbolicErr != nil || strings.TrimSpace(symbolic) != branch {
		return "", "", false, false, errors.New("project Git HEAD is invalid")
	}
	_, refErr := run("show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	var exitCode interface{ ExitCode() int }
	if !errors.As(refErr, &exitCode) || exitCode.ExitCode() != 1 || ctx.Err() != nil {
		return "", "", false, false, errors.New("project Git HEAD is invalid")
	}
	return "", branch, true, false, nil
}
