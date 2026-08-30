package main

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/buildinfo"
)

func TestStampSiteCommitFromEnvironment(t *testing.T) {
	oldCommit := buildinfo.Commit
	t.Cleanup(func() { buildinfo.Commit = oldCommit })

	const commit = "4a57701f872d81b48be743e69417e36c429f7bad"
	buildinfo.Commit = "unknown"
	t.Setenv("AEONTRA_SITE_COMMIT", commit)
	stampSiteCommitFromEnvironment()
	if buildinfo.Commit != commit {
		t.Fatalf("commit = %q, want %q", buildinfo.Commit, commit)
	}
}

func TestStampSiteCommitFallsBackToCoolifySourceCommit(t *testing.T) {
	oldCommit := buildinfo.Commit
	t.Cleanup(func() { buildinfo.Commit = oldCommit })

	const commit = "5c1ee5b59f45b40cfa77a891271ada7fd1573855"
	buildinfo.Commit = "unknown"
	t.Setenv("AEONTRA_SITE_COMMIT", "$SOURCE_COMMIT")
	t.Setenv("SOURCE_COMMIT", commit)
	stampSiteCommitFromEnvironment()
	if buildinfo.Commit != commit {
		t.Fatalf("commit = %q, want Coolify source commit %q", buildinfo.Commit, commit)
	}
}

func TestStampSiteCommitRejectsInvalidValuesAndPreservesBakedIdentity(t *testing.T) {
	oldCommit := buildinfo.Commit
	t.Cleanup(func() { buildinfo.Commit = oldCommit })

	for _, value := range []string{"", "unknown", "4A57701F872D81B48BE743E69417E36C429F7BAD", "../commit"} {
		buildinfo.Commit = "unknown"
		t.Setenv("AEONTRA_SITE_COMMIT", value)
		t.Setenv("SOURCE_COMMIT", "")
		stampSiteCommitFromEnvironment()
		if buildinfo.Commit != "unknown" {
			t.Fatalf("invalid value %q changed commit to %q", value, buildinfo.Commit)
		}
	}

	buildinfo.Commit = "baked"
	t.Setenv("AEONTRA_SITE_COMMIT", "4a57701f872d81b48be743e69417e36c429f7bad")
	stampSiteCommitFromEnvironment()
	if buildinfo.Commit != "baked" {
		t.Fatalf("baked commit changed to %q", buildinfo.Commit)
	}
}
