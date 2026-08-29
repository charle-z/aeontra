package edgeclient

import (
	"slices"
	"strings"
	"testing"
)

func TestDevGitNetworkCommandAcceptsOnlyOwnerBoundCanonicalOperations(t *testing.T) {
	remote := "https://github.com/charle-z/project.git"
	for _, args := range [][]string{
		{"clone", "--single-branch", "--", remote, "."},
		{"fetch", "--no-tags", remote, "refs/heads/main:refs/remotes/origin/main"},
		{"push", "--porcelain", remote, strings.Repeat("a", 40) + ":refs/heads/main"},
		{"ls-remote", "--heads", remote, "refs/heads/main"},
	} {
		got, network, err := devGitNetworkCommand(args, "charle-z")
		if err != nil || !network || got != remote {
			t.Fatalf("args=%q remote=%q network=%t err=%v", args, got, network, err)
		}
	}
	for _, args := range [][]string{
		{"fetch", "--no-tags", "https://attacker.example/project.git", "refs/heads/main:refs/remotes/origin/main"},
		{"push", "--porcelain", "origin", "main"},
		{"ls-remote", "--heads", "ssh://github.com/charle-z/project.git", "refs/heads/main"},
		{"pull", remote, "main"},
	} {
		if _, network, err := devGitNetworkCommand(args, "charle-z"); err == nil || network {
			t.Fatalf("unsafe network args accepted: %q", args)
		}
	}
	if remote, network, err := devGitNetworkCommand([]string{"status", "--porcelain"}, "charle-z"); err != nil || network || remote != "" {
		t.Fatalf("local command classified as network: remote=%q network=%t err=%v", remote, network, err)
	}
}

func TestDevGitProtectedArgumentsNeutralizeRepositoryTransportConfig(t *testing.T) {
	remote := "https://github.com/charle-z/project.git"
	args := devGitProtectedArguments([]string{"ls-remote", "--heads", remote, "refs/heads/main"}, remote)
	for _, required := range []string{
		"credential.helper=",
		"credential." + remote + ".helper=",
		"http.proxy=",
		"http." + remote + ".proxy=",
		"http.extraHeader=",
		"http." + remote + ".extraHeader=",
		"http.sslVerify=true",
		"http." + remote + ".sslVerify=true",
		"http.followRedirects=false",
		"url." + remote + ".insteadOf=" + remote,
	} {
		if !slices.Contains(args, required) {
			t.Errorf("protected Git arguments omit %q: %q", required, args)
		}
	}
	if got := args[len(args)-4:]; !slices.Equal(got, []string{"ls-remote", "--heads", remote, "refs/heads/main"}) {
		t.Fatalf("Git operation changed: %q", got)
	}
}

func TestDevGitReadsLocalConfigOnlyForExactRemoteInspection(t *testing.T) {
	if !devGitReadsLocalConfig(devGitRemoteConfigArguments) {
		t.Fatal("exact owner-remote inspection did not read repository config")
	}
	for _, args := range [][]string{
		{"config", "--local", "--list"},
		{"config", "--local", "--no-includes", "--get-regexp", `^url\.`},
		{"status", "--porcelain=v1"},
		{"fetch", "--no-tags", "https://github.com/charle-z/project.git", "refs/heads/main:refs/remotes/origin/main"},
	} {
		if devGitReadsLocalConfig(args) {
			t.Fatalf("unexpected command may read repository config: %q", args)
		}
	}
}

func TestDevGitFastForwardArgumentsAreExact(t *testing.T) {
	commit := "0123456789abcdef0123456789abcdef01234567"
	if !devGitFastForwardArguments([]string{"merge", "--ff-only", commit}) {
		t.Fatal("exact fast-forward was rejected")
	}
	for _, args := range [][]string{
		{"merge", commit},
		{"merge", "--ff-only", "main"},
		{"merge", "--ff-only", commit, "--no-verify"},
	} {
		if devGitFastForwardArguments(args) {
			t.Fatalf("unsafe fast-forward accepted: %q", args)
		}
	}
}
