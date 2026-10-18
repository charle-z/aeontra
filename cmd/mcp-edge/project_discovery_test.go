package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectDiscoverReturnsSafeRecoveryDecisionWithoutCandidatePath(t *testing.T) {
	home := t.TempDir()
	roots := edgeclient.WorkspaceRoots{
		Dev: filepath.Join(home, "workspaces"), HTBLinux: filepath.Join(home, "htb-machines"),
	}
	legacy := filepath.Join(roots.Dev, "legacy-name")
	for _, path := range []string{legacy, roots.HTBLinux} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldOpen := openLocalProjectDiscovery
	openLocalProjectDiscovery = func() (localProjectDiscovery, error) {
		return localProjectDiscovery{
			owner: "charle-z", roots: roots,
			inspector: &mutableProjectInspector{state: edgeclient.ProjectCheckoutReady},
		}, nil
	}
	t.Cleanup(func() { openLocalProjectDiscovery = oldOpen })
	var stdout bytes.Buffer
	if err := projectCommand([]string{"discover", "--alias", "PROJECT", "--repository", "REPO"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, forbidden := range []string{legacy, roots.Dev, "candidate_path", "workspace_id", "device_id", "runtime_id"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("project discovery exposed %q: %s", forbidden, output)
		}
	}
	for _, required := range []string{"project", "charle-z/repo", "associate_existing", `"candidate_count":1`} {
		if !strings.Contains(output, required) {
			t.Fatalf("project discovery missing %q: %s", required, output)
		}
	}
}

func TestProjectDiscoverRejectsMutationAndOpaqueArgumentsBeforeOpeningContext(t *testing.T) {
	oldOpen := openLocalProjectDiscovery
	called := false
	openLocalProjectDiscovery = func() (localProjectDiscovery, error) {
		called = true
		return localProjectDiscovery{}, nil
	}
	t.Cleanup(func() { openLocalProjectDiscovery = oldOpen })
	for _, args := range [][]string{
		{"discover", "--alias", "project"},
		{"discover", "--alias", "project", "--repository", "repo", "--target", "parrot"},
		{"discover", "--alias", "project", "--repository", "repo", "--state", "/tmp/state"},
		{"discover", "--alias", "project", "--repository", "repo", "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		if err := projectCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("unsafe discovery arguments accepted: %v", args)
		}
	}
	if called {
		t.Fatal("invalid project discovery arguments opened local context")
	}
}
