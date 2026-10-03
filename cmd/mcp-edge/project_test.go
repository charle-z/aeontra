package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectPrepareCLIUsesBoundedCanonicalPreparation(t *testing.T) {
	oldPrepare := prepareLocalProject
	t.Cleanup(func() { prepareLocalProject = oldPrepare })
	called := false
	prepareLocalProject = func(ctx context.Context, alias, repository, target string) (edgeclient.ProjectStatus, error) {
		called = true
		if _, ok := ctx.Deadline(); !ok || alias != "project" || repository != "repo" || target != "parrot" {
			t.Fatalf("unbounded or changed preparation: %q %q %q", alias, repository, target)
		}
		return edgeclient.ProjectStatus{Alias: alias, Repository: "charle-z/" + repository, Target: target, State: "ready"}, nil
	}
	var stdout bytes.Buffer
	if err := projectCommand([]string{"prepare", "--alias", "project", "--repository", "repo", "--target", "parrot"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(stdout.String(), `"state":"ready"`) {
		t.Fatalf("prepare was not completed: %s", stdout.String())
	}
}

func TestProjectPrepareCLIRejectsIncompleteAndFreePathArguments(t *testing.T) {
	oldPrepare := prepareLocalProject
	t.Cleanup(func() { prepareLocalProject = oldPrepare })
	prepareLocalProject = func(context.Context, string, string, string) (edgeclient.ProjectStatus, error) {
		t.Fatal("invalid preparation opened local authority")
		return edgeclient.ProjectStatus{}, nil
	}
	for _, args := range [][]string{
		{"prepare", "--alias", "project"},
		{"prepare", "--alias", "project", "--repository", "repo"},
		{"prepare", "--alias", "project", "--repository", "repo", "--target", "parrot", "/tmp/repo"},
		{"prepare", "--alias", "project", "--repository", "repo", "--target", "parrot", "--state", "/tmp/state"},
	} {
		if err := projectCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("invalid preparation accepted: %v", args)
		}
	}
}

type mutableProjectInspector struct {
	state edgeclient.ProjectCheckoutState
}

func (m *mutableProjectInspector) Inspect(context.Context, string, string, string) (edgeclient.ProjectCheckoutState, error) {
	return m.state, nil
}

func TestProjectStatusAndResolveOutputContainsNoOpaqueIdentifiersOrPaths(t *testing.T) {
	for _, operation := range []string{"status", "resolve"} {
		t.Run(operation, func(t *testing.T) {
			stores, workspace, inspector := newProjectCommandFixture(t)
			oldOpen := openLocalProjectStores
			openLocalProjectStores = func() (*localProjectStores, error) { return stores, nil }
			t.Cleanup(func() { openLocalProjectStores = oldOpen })
			inspector.state = edgeclient.ProjectCheckoutReady
			var stdout bytes.Buffer
			if err := projectCommand([]string{operation, "--alias", "EKOPARTY", "--target", "PARROT"}, &stdout, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			output := stdout.String()
			for _, forbidden := range []string{workspace.ID, workspace.Path, "workspace_id", "device_id", "runtime_id", "job_id"} {
				if strings.Contains(output, forbidden) {
					t.Fatalf("project output exposed %q: %s", forbidden, output)
				}
			}
			for _, required := range []string{"ekoparty", "charle-z/ekoparty-trip-agent", "parrot", "ready", "linux-workcell", "dev"} {
				if !strings.Contains(output, required) {
					t.Fatalf("project output missing %q: %s", required, output)
				}
			}
		})
	}
}

func TestProjectStatusAndResolveReportDirtyCheckoutWithoutExposingPaths(t *testing.T) {
	for _, operation := range []string{"status", "resolve"} {
		t.Run(operation, func(t *testing.T) {
			stores, workspace, inspector := newProjectCommandFixture(t)
			oldOpen := openLocalProjectStores
			openLocalProjectStores = func() (*localProjectStores, error) { return stores, nil }
			t.Cleanup(func() { openLocalProjectStores = oldOpen })
			inspector.state = edgeclient.ProjectCheckoutDirty

			var stdout bytes.Buffer
			if err := projectCommand([]string{operation, "--alias", "ekoparty"}, &stdout, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), `"state":"dirty"`) || !strings.Contains(stdout.String(), `"reason":"normal_workspace_changes"`) {
				t.Fatalf("dirty %s=%q", operation, stdout.String())
			}
			for _, forbidden := range []string{workspace.ID, workspace.Path} {
				if strings.Contains(stdout.String(), forbidden) {
					t.Fatalf("dirty %s exposed %q: %s", operation, forbidden, stdout.String())
				}
			}
		})
	}
}

func TestProjectCommandRejectsFreeFormPathsAndOpaqueArgumentsBeforeOpeningStores(t *testing.T) {
	oldOpen := openLocalProjectStores
	called := false
	openLocalProjectStores = func() (*localProjectStores, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { openLocalProjectStores = oldOpen })
	for _, args := range [][]string{
		nil,
		{"register", "--alias", "project"},
		{"status"},
		{"status", "--alias", "project", "--state", "/tmp/state"},
		{"resolve", "--alias", "project", "ws_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		if err := projectCommand(args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
			t.Fatalf("unsafe project arguments accepted: %v", args)
		}
	}
	if called {
		t.Fatal("invalid project arguments opened local stores")
	}
}

func newProjectCommandFixture(t *testing.T) (*localProjectStores, edgeclient.Workspace, *mutableProjectInspector) {
	t.Helper()
	state := t.TempDir()
	home := t.TempDir()
	devRoot := filepath.Join(home, "workspaces")
	htbRoot := filepath.Join(home, "htb-machines")
	workspacePath := filepath.Join(devRoot, "ekoparty-trip-agent")
	for _, path := range []string{workspacePath, htbRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(workspacePath, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspaces, err := edgeclient.OpenWorkspaceRegistryWithRoots(state, edgeclient.WorkspaceRoots{Dev: devRoot, HTBLinux: htbRoot})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, err := workspaces.AddProfile(workspacePath, edgeclient.WorkspaceProfileLinuxWorkcell)
	if err != nil {
		_ = workspaces.Close()
		t.Fatal(err)
	}
	inspector := &mutableProjectInspector{state: edgeclient.ProjectCheckoutReady}
	projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector,
	})
	if err != nil {
		_ = workspaces.Close()
		t.Fatal(err)
	}
	if _, _, err := projects.Register(edgeclient.ProjectRegistration{
		Alias: "ekoparty", Owner: "charle-z", Repository: "ekoparty-trip-agent",
		PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []edgeclient.WorkspaceProfile{edgeclient.WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		_ = projects.Close()
		_ = workspaces.Close()
		t.Fatal(err)
	}
	return &localProjectStores{projects: projects, workspaces: workspaces}, workspace, inspector
}
