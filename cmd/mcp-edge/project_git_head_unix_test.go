//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserveProjectHeadAgainstRealUnbornGit(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git is unavailable")
	}
	dir := t.TempDir()
	command := exec.Command(gitPath, "init", "--quiet", "--initial-branch=main", dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("initialize local repository: %v: %s", err, output)
	}
	run := func(args ...string) (string, error) {
		command := exec.Command(gitPath, args...)
		command.Dir = dir
		output, err := command.CombinedOutput()
		return string(output), err
	}
	head, branch, unborn, detached, err := observeProjectHead(context.Background(), run, projectSnapshotHeadPattern.MatchString, validProjectSnapshotBranch)
	if err != nil || head != "" || branch != "main" || !unborn || detached {
		t.Fatalf("empty repository: head=%q branch=%q unborn=%t detached=%t err=%v", head, branch, unborn, detached, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--quiet", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	head, branch, unborn, detached, err = observeProjectHead(context.Background(), run, projectSnapshotHeadPattern.MatchString, validProjectSnapshotBranch)
	if err != nil || !projectSnapshotHeadPattern.MatchString(head) || branch != "main" || unborn || detached {
		t.Fatalf("committed repository: head=%q branch=%q unborn=%t detached=%t err=%v", head, branch, unborn, detached, err)
	}
	if _, err := run("checkout", "--quiet", "--detach", head); err != nil {
		t.Fatal(err)
	}
	detachedHead, detachedBranch, detachedUnborn, isDetached, err := observeProjectHead(context.Background(), run, projectSnapshotHeadPattern.MatchString, validProjectSnapshotBranch)
	if err != nil || !strings.EqualFold(detachedHead, head) || detachedBranch != "" || detachedUnborn || !isDetached {
		t.Fatalf("detached repository: head=%q branch=%q unborn=%t detached=%t err=%v", detachedHead, detachedBranch, detachedUnborn, isDetached, err)
	}
}
