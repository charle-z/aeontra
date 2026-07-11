package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationProfileIsClosedAndHardened(t *testing.T) {
	c := config{root: "/repos", hostRoot: "/host/repos", image: "node:22-alpine", store: "store", user: "10001:10001"}
	args, err := c.argv("/repos/demo", "pnpm-validate")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--network none", "--read-only", "--cap-drop ALL", "no-new-privileges", "--user 10001:10001", "pnpm install --offline --frozen-lockfile --ignore-scripts"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if !strings.Contains(joined, "src=/host/repos/demo,dst=/workspace") {
		t.Fatalf("runner used container path instead of host path: %q", joined)
	}
	if _, err := c.argv("/repos/demo", "anything-from-agent"); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestLockfileProfileHasOnlyFixedRegistryNetwork(t *testing.T) {
	c := config{root: "/repos", hostRoot: "/host/repos", image: "node:22-alpine", store: "store", user: "10001:10001"}
	args, err := c.argv("/repos/demo", "pnpm-lockfile")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--network bridge") || !strings.Contains(joined, "--ignore-scripts --registry=https://registry.npmjs.org") {
		t.Fatalf("unexpected lockfile argv: %s", joined)
	}
}

func TestRepoPathRejectsTraversalAndOnlyAcceptsDirectChild(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "portfolio")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := config{root: root}
	if got, err := c.repoPath("portfolio"); err != nil || got != repo {
		t.Fatalf("repoPath = %q, %v", got, err)
	}
	for _, bad := range []string{"..", "portfolio/child", "../portfolio", "."} {
		if _, err := c.repoPath(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestBearerComparison(t *testing.T) {
	if !constantBearer("Bearer 01234567890123456789012345678901", "01234567890123456789012345678901") {
		t.Fatal("valid bearer rejected")
	}
	if constantBearer("Bearer nope", "01234567890123456789012345678901") {
		t.Fatal("invalid bearer accepted")
	}
}
