package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistryCreatesPrivateFileAndRejectsWorkspaceReuse(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
		Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	info, err := os.Lstat(filepath.Join(state, projectRegistryFile))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("project registry mode=%v", info.Mode())
	}
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "first", Owner: "charle-z", Repository: "first", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = registry.Register(ProjectRegistration{
		Alias: "second", Owner: "charle-z", Repository: "second", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	})
	if !projectErrorIs(err, ProjectErrorWorkspaceConflict) {
		t.Fatalf("workspace reuse err=%v", err)
	}
}

func TestProjectResolutionBlocksMissingWorkspaceAndProfileDrift(t *testing.T) {
	t.Run("missing workspace", func(t *testing.T) {
		state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
		registry, err := OpenProjectRegistry(ProjectRegistryConfig{
			StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
			Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer registry.Close()
		registerProjectFixture(t, registry, workspace)
		if err := os.RemoveAll(workspace.Path); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Resolve(context.Background(), "project", ""); !projectErrorIs(err, ProjectErrorWorkspaceMissing) {
			t.Fatalf("missing workspace err=%v", err)
		}
	})

	t.Run("profile drift", func(t *testing.T) {
		state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
		registry, err := OpenProjectRegistry(ProjectRegistryConfig{
			StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
			Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
		})
		if err != nil {
			t.Fatal(err)
		}
		defer registry.Close()
		registerProjectFixture(t, registry, workspace)
		if _, err := workspaces.db.Exec(`UPDATE workspace_configs SET profile=? WHERE workspace_id=?`, WorkspaceProfileSandbox, workspace.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Resolve(context.Background(), "project", ""); !projectErrorIs(err, ProjectErrorProfileDenied) {
			t.Fatalf("profile drift err=%v", err)
		}
	})
}

func registerProjectFixture(t *testing.T, registry *ProjectRegistry, workspace Workspace) {
	t.Helper()
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
}
