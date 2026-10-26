//go:build !windows

package main

import (
	"context"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func resolveDirectProjectWorkspace(ctx context.Context, stateRoot string, credential edgeclient.GitHubCredential, workspaces *edgeclient.WorkspaceRegistry, roots edgeclient.WorkspaceRoots, resolved edgeclient.ProjectResolution, id string) (edgeclient.ProjectResolution, error) {
	if id == "" {
		return resolved, nil
	}
	manager, err := edgeclient.OpenProjectWorktreeManager(edgeclient.ProjectWorktreeManagerConfig{StateRoot: stateRoot, Roots: roots, Workspaces: workspaces,
		Runner: edgeclient.NewDevGitCommandRunner(stateRoot, "/usr/local/bin:/usr/bin:/bin"), Credential: credential})
	if err != nil {
		return edgeclient.ProjectResolution{}, err
	}
	defer manager.Close()
	return manager.ResolveDirectWorkspace(ctx, resolved, id)
}
