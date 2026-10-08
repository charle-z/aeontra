//go:build !windows

package edgeclient

import (
	"slices"
	"testing"
)

func TestContainedDevGitFastForwardIsNetworklessAndWorkspaceScoped(t *testing.T) {
	workspace := t.TempDir()
	commit := "0123456789abcdef0123456789abcdef01234567"
	args, err := containedDevGitFastForwardArgs(workspace, "/usr/bin/git", []string{"merge", "--ff-only", commit})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "--unshare-all") || slices.Contains(args, "--share-net") {
		t.Fatalf("network namespace is not isolated: %q", args)
	}
	bind := slices.Index(args, "--bind")
	if bind < 0 || bind+2 >= len(args) || args[bind+1] != workspace || args[bind+2] != "/workspace" {
		t.Fatalf("workspace bind is not exact: %q", args)
	}
	if !slices.Contains(args, "core.hooksPath=/dev/null") || !slices.Contains(args, commit) {
		t.Fatalf("Git mutation is not exact and hook-disabled: %q", args)
	}
	for index, arg := range args {
		if arg == "--bind" && index != bind {
			t.Fatalf("unexpected writable host bind: %q", args)
		}
	}
}

func TestContainedDevGitFastForwardRejectsOtherGitCommands(t *testing.T) {
	if _, err := containedDevGitFastForwardArgs(t.TempDir(), "/usr/bin/git", []string{"checkout", "main"}); err == nil {
		t.Fatal("arbitrary Git mutation accepted")
	}
}
