//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistryListClaimsDetectsActualRemoteConfigMutation(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	runProjectGit(t, workspace.Path, "init")
	runProjectGit(t, workspace.Path, "config", "user.name", "MCP Devbox Test")
	runProjectGit(t, workspace.Path, "config", "user.email", "test@localhost")
	if err := os.WriteFile(filepath.Join(workspace.Path, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, workspace.Path, "add", "README.md")
	runProjectGit(t, workspace.Path, "commit", "-m", "fixture")
	runProjectGit(t, workspace.Path, "remote", "add", "origin", "https://github.com/charle-z/repo.git")
	inspector := localProjectCheckoutInspector{roots: &workspaces.roots}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot",
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimHealthy {
		t.Fatalf("initial claims=%+v err=%v", claims, err)
	}
	runProjectGit(t, workspace.Path, "remote", "set-url", "origin", "https://github.com/other/repo.git")
	claims, err = registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimStale || claims[0].Reason != ProjectErrorRepositoryMismatch {
		t.Fatalf("claims after remote mutation=%+v err=%v", claims, err)
	}
	status, err := registry.Status(context.Background(), "project", "parrot")
	if err != nil || status.Reason != ProjectErrorRepositoryMismatch {
		t.Fatalf("status after remote mutation=%+v err=%v", status, err)
	}
	if err := registry.ReleaseClaim("project", "charle-z", "repo", "parrot", claims[0].Generation); err != nil {
		t.Fatalf("release after remote mutation: %v", err)
	}
	if _, err := os.Stat(workspace.Path); err != nil {
		t.Fatalf("release removed source workspace: %v", err)
	}
	claims, err = registry.ListClaims()
	if err != nil || len(claims) != 0 {
		t.Fatalf("claims after remote-mismatch release=%+v err=%v", claims, err)
	}
}
