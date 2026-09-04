package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixedProjectInspector struct {
	state ProjectCheckoutState
}

func (f fixedProjectInspector) Inspect(context.Context, string, string, string) (ProjectCheckoutState, error) {
	return f.state, nil
}

func TestProjectAliasAndRepositoryNormalizationRejectsAmbiguity(t *testing.T) {
	alias, err := NormalizeProjectAlias("  Ekoparty-Trip-Agent ")
	if err != nil || alias != "ekoparty-trip-agent" {
		t.Fatalf("alias=%q err=%v", alias, err)
	}
	owner, repository, err := NormalizeProjectRepository("CHARLE-Z", "Ekoparty-Trip-Agent")
	if err != nil || owner != "charle-z" || repository != "ekoparty-trip-agent" {
		t.Fatalf("repository=%s/%s err=%v", owner, repository, err)
	}
	for _, input := range []string{"", "-project", "project-", "project_name", "../project", "еkoparty", strings.Repeat("a", 65)} {
		if _, err := NormalizeProjectAlias(input); err == nil {
			t.Fatalf("unsafe alias accepted: %q", input)
		}
	}
	for _, input := range []string{"../repo", ".repo", "repo/other", "répo", strings.Repeat("a", 101)} {
		if _, _, err := NormalizeProjectRepository("charle-z", input); err == nil {
			t.Fatalf("unsafe repository accepted: %q", input)
		}
	}
}

func TestProjectRegistryPersistsAliasAndResolvesWithoutOpaqueOutput(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
		Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	project, created, err := registry.Register(ProjectRegistration{
		Alias: "Ekoparty-Trip-Agent", Owner: "CHARLE-Z", Repository: "Ekoparty-Trip-Agent",
		PreferredTarget: "Parrot", TargetAlias: "PARROT", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	})
	if err != nil || !created {
		t.Fatalf("register created=%v err=%v", created, err)
	}
	if project.Alias != "ekoparty-trip-agent" || project.Repository != "ekoparty-trip-agent" || project.PreferredTarget != "parrot" {
		t.Fatalf("project=%+v", project)
	}
	if err := registry.Close(); err != nil {
		t.Fatal(err)
	}

	registry, err = OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
		Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	resolution, err := registry.Resolve(context.Background(), "EKOPARTY-TRIP-AGENT", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Project.Alias != "ekoparty-trip-agent" || resolution.TargetAlias != "parrot" || resolution.Workspace.ID != workspace.ID || resolution.CheckoutState != ProjectCheckoutReady {
		t.Fatalf("resolution=%+v", resolution)
	}
	status := resolution.SafeStatus()
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, forbidden := range []string{workspace.ID, workspace.Path, state, "workspace_id", "path"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("safe project status exposed %q: %s", forbidden, text)
		}
	}
	for _, required := range []string{"ekoparty-trip-agent", "charle-z/ekoparty-trip-agent", "parrot", "ready", "linux-workcell", "dev"} {
		if !strings.Contains(text, required) {
			t.Fatalf("safe project status missing %q: %s", required, text)
		}
	}
}

func TestProjectRegistryIsIdempotentAndRejectsAliasRepositoryAndOwnerConflicts(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
		Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	base := ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}
	if _, created, err := registry.Register(base); err != nil || !created {
		t.Fatalf("first register created=%v err=%v", created, err)
	}
	if _, created, err := registry.Register(base); err != nil || created {
		t.Fatalf("repeat register created=%v err=%v", created, err)
	}
	changed := base
	changed.Repository = "other"
	if _, _, err := registry.Register(changed); !projectErrorIs(err, ProjectErrorAliasConflict) {
		t.Fatalf("alias conflict err=%v", err)
	}
	changed = base
	changed.Alias = "other-alias"
	if _, _, err := registry.Register(changed); !projectErrorIs(err, ProjectErrorRepositoryConflict) {
		t.Fatalf("repository conflict err=%v", err)
	}
	changed = base
	changed.Alias = "other-project"
	changed.Repository = "other-project"
	changed.Owner = "another-owner"
	if _, _, err := registry.Register(changed); !projectErrorIs(err, ProjectErrorOwnerDenied) {
		t.Fatalf("owner conflict err=%v", err)
	}
}

func TestProjectResolutionRevalidatesProfileTargetAndCheckout(t *testing.T) {
	for _, test := range []struct {
		name      string
		state     ProjectCheckoutState
		wantState ProjectCheckoutState
		wantError ProjectErrorCode
	}{
		{name: "dirty", state: ProjectCheckoutDirty, wantState: ProjectCheckoutDirty},
		{name: "remote mismatch", state: ProjectCheckoutRemoteMismatch, wantError: ProjectErrorRepositoryMismatch},
		{name: "unsafe", state: ProjectCheckoutUnsafe, wantError: ProjectErrorCheckoutUnsafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
			registry, err := OpenProjectRegistry(ProjectRegistryConfig{
				StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
				Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer registry.Close()
			if _, _, err := registry.Register(ProjectRegistration{
				Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot",
				TargetAlias: "parrot", WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
			}); err != nil {
				t.Fatal(err)
			}
			registry.inspector = fixedProjectInspector{state: test.state}
			resolution, err := registry.Resolve(context.Background(), "project", "parrot")
			if test.wantError != "" {
				if !projectErrorIs(err, test.wantError) {
					t.Fatalf("resolve err=%v", err)
				}
			} else if err != nil || resolution.CheckoutState != test.wantState || resolution.SafeStatus().State != string(test.wantState) {
				t.Fatalf("resolution=%+v err=%v", resolution, err)
			} else if resolution.CheckoutDiagnostic == nil || resolution.CheckoutDiagnostic.Reason != "normal_workspace_changes" {
				t.Fatalf("dirty checkout diagnostic=%+v", resolution.CheckoutDiagnostic)
			}
			if _, err := registry.Resolve(context.Background(), "project", "other"); !projectErrorIs(err, ProjectErrorTargetNotFound) {
				t.Fatalf("target err=%v", err)
			}
		})
	}
}

func TestProjectRegistrationRejectsDisallowedWorkspaceProfile(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces,
		Inspector: fixedProjectInspector{state: ProjectCheckoutReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	_, _, err = registry.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot",
		TargetAlias: "parrot", WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileSandbox},
	})
	if !projectErrorIs(err, ProjectErrorProfileDenied) {
		t.Fatalf("profile mismatch err=%v", err)
	}
}

func TestCanonicalProjectPathIsPureAndStaysUnderDevelopmentRoot(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "workspaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := CanonicalProjectPath(WorkspaceRoots{Dev: root, HTBLinux: filepath.Join(home, "htb-machines")}, "Ekoparty-Trip-Agent")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(root, "ekoparty-trip-agent") {
		t.Fatalf("path=%q", path)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canonical inference mutated filesystem: %v", err)
	}
	if _, err := CanonicalProjectPath(WorkspaceRoots{Dev: root, HTBLinux: filepath.Join(home, "htb-machines")}, "../escape"); err == nil {
		t.Fatal("unsafe repository produced a canonical path")
	}
}

func newProjectRegistryFixture(t *testing.T, profile WorkspaceProfile) (string, *WorkspaceRegistry, Workspace) {
	t.Helper()
	state := t.TempDir()
	home := t.TempDir()
	devRoot := filepath.Join(home, "workspaces")
	htbRoot := filepath.Join(home, "htb-machines")
	workspacePath := filepath.Join(devRoot, "repo")
	for _, path := range []string{workspacePath, htbRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(workspacePath, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	registry, err := OpenWorkspaceRegistryWithRoots(state, WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registry.Close() })
	workspace, _, err := registry.AddProfile(workspacePath, profile)
	if err != nil {
		t.Fatal(err)
	}
	return state, registry, workspace
}
