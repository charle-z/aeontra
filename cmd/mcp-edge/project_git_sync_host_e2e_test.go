//go:build !windows

package main

import (
	"context"
	"os"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectGitStatusAgainstRealOwnerBoundRemote(t *testing.T) {
	if os.Getenv("PROJECT_GIT_HOST_E2E") != "1" {
		t.Skip("host project Git acceptance is explicit")
	}
	stateRoot := os.Getenv("PROJECT_GIT_STATE_ROOT")
	alias := os.Getenv("PROJECT_GIT_ALIAS")
	target := os.Getenv("PROJECT_GIT_TARGET")
	if stateRoot == "" || alias == "" || target == "" {
		t.Fatal("PROJECT_GIT_STATE_ROOT, PROJECT_GIT_ALIAS, and PROJECT_GIT_TARGET are required")
	}

	credential, workspaces, projects, _, code := openProjectControlState(stateRoot)
	if code != "" {
		t.Fatalf("open project control state: %s", code)
	}
	defer workspaces.Close()
	defer projects.Close()
	resolved, err := projects.Resolve(context.Background(), alias, target)
	if err != nil {
		t.Fatal(err)
	}
	result, err := inspectProjectGitCheckout(
		context.Background(),
		resolved,
		edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		credential,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectAlias != alias || result.ProjectTarget != target || result.GitHead == "" || result.GitBranch == "" {
		t.Fatalf("incomplete project Git result: %+v", result)
	}
}
