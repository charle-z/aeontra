package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectAssociationRollsBackNewWorkspaceWhenProjectBindingFails(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	existingPath := filepath.Join(roots.Dev, "existing")
	candidatePath := filepath.Join(roots.Dev, "legacy-second")
	for _, path := range []string{existingPath, candidatePath} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(path, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	states := map[string]ProjectCheckoutState{
		existingPath:  ProjectCheckoutReady,
		candidatePath: ProjectCheckoutRemoteMismatch,
	}
	inspector := pathProjectInspector{states: states}
	workspaces, err := OpenWorkspaceRegistryWithRoots(state, roots)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	projects, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer projects.Close()
	existingWorkspace, _, err := workspaces.AddProfile(existingPath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := projects.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "first", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: existingWorkspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	states[existingPath] = ProjectCheckoutRemoteMismatch
	states[candidatePath] = ProjectCheckoutReady
	config := ProjectAssociationConfig{
		Projects: projects, Workspaces: workspaces, Roots: roots, Inspector: inspector,
	}
	plan, err := PlanProjectAssociation(context.Background(), config, ProjectAssociationRequest{
		Alias: "project", Owner: "charle-z", Repository: "second", TargetAlias: "parrot",
		Profile: WorkspaceProfileLinuxWorkcell,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyProjectAssociation(context.Background(), config, plan); !projectErrorIs(err, ProjectErrorAliasConflict) {
		t.Fatalf("association binding err=%v", err)
	}
	items, err := workspaces.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != existingWorkspace.ID || items[0].Path != existingPath {
		t.Fatalf("failed association left workspace mutation: %+v", items)
	}
	if info, err := os.Lstat(candidatePath); err != nil || !info.IsDir() {
		t.Fatalf("failed association changed checkout: %v info=%v", err, info)
	}
}
