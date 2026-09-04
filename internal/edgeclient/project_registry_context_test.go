//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// registryDBProbeInspector makes sure registry listing has released its
// single SQLite connection before performing the optional identity check.
type registryDBProbeInspector struct {
	db       *sql.DB
	queryErr error
}

func (i *registryDBProbeInspector) Inspect(context.Context, string, string, string) (ProjectCheckoutState, error) {
	return ProjectCheckoutReady, nil
}

func (i *registryDBProbeInspector) InspectRepositoryIdentity(ctx context.Context, _ string, _, _ string) (ProjectCheckoutObservation, error) {
	var value int
	i.queryErr = i.db.QueryRowContext(ctx, `SELECT 1`).Scan(&value)
	return ProjectCheckoutObservation{State: ProjectCheckoutReady}, i.queryErr
}

func TestProjectRegistryListClaimsContextFiltersTargetAndUsesRequestContext(t *testing.T) {
	state, workspaces, firstWorkspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	secondPath := filepath.Join(filepath.Dir(firstWorkspace.Path), "second-repo")
	if err := os.MkdirAll(filepath.Join(secondPath, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	secondWorkspace, _, err := workspaces.AddProfile(secondPath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	inspector := fixedProjectInspector{state: ProjectCheckoutReady}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	for _, registration := range []ProjectRegistration{
		{Alias: "parrot-project", Owner: "charle-z", Repository: "parrot-repo", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: firstWorkspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}},
		{Alias: "windows-project", Owner: "charle-z", Repository: "windows-repo", PreferredTarget: "windows", TargetAlias: "windows", WorkspaceID: secondWorkspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}},
	} {
		if _, _, err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
	}
	claims, err := registry.ListClaimsContext(context.Background(), "PARROT")
	if err != nil || len(claims) != 1 || claims[0].Alias != "parrot-project" || claims[0].Target != "parrot" {
		t.Fatalf("target-filtered claims=%+v err=%v", claims, err)
	}
	if _, err := registry.ListClaimsContext(context.Background(), "not a target"); !projectErrorIs(err, ProjectErrorInvalidInput) {
		t.Fatalf("invalid target err=%v", err)
	}

	// The operation context must reach the bounded repository identity check.
	probe := &registryDBProbeInspector{}
	registry.inspector = probe
	probe.db = registry.db
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	claims, err = registry.ListClaimsContext(ctx, "parrot")
	if err != nil || len(claims) != 1 || probe.queryErr != nil {
		t.Fatalf("context-aware claims=%+v err=%v probe=%v", claims, err, probe.queryErr)
	}
}

func TestProjectRegistryListClaimsContextExposesUnboundPreferredTargetForRecovery(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "stale-project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.db.Exec(`DELETE FROM project_workspaces WHERE alias=?`, "stale-project"); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaimsContext(context.Background(), "parrot")
	if err != nil || len(claims) != 1 || claims[0].Alias != "stale-project" || claims[0].Target != "parrot" || !claims[0].Repairable {
		t.Fatalf("unbound recovery claim=%+v err=%v", claims, err)
	}
	if err := registry.ReleaseClaim("stale-project", "charle-z", "repo", "parrot", claims[0].Generation); err != nil {
		t.Fatalf("release unbound claim: %v", err)
	}
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "stale-project", Owner: "charle-z", Repository: "replacement", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatalf("released alias remained phantom-conflicted: %v", err)
	}
}

type attestationMutatingInspector struct {
	path   string
	mutate bool
}

func (i *attestationMutatingInspector) Inspect(_ context.Context, _ string, _, _ string) (ProjectCheckoutState, error) {
	if i.mutate {
		oldPath := i.path + ".before-reconcile"
		if err := os.Rename(filepath.Join(i.path, ".git"), oldPath); err != nil {
			return ProjectCheckoutUnavailable, err
		}
		if err := os.Mkdir(filepath.Join(i.path, ".git"), 0o700); err != nil {
			return ProjectCheckoutUnavailable, err
		}
		i.mutate = false
	}
	return ProjectCheckoutReady, nil
}

func TestProjectRegistryReconcileRejectsWorkspaceChangedDuringInspection(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	inspector := &attestationMutatingInspector{path: workspace.Path}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID,
		AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	inspector.mutate = true
	_, err = registry.ReconcileClaim(context.Background(), "project", "parrot")
	var projectFailure *ProjectError
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorPlanChanged || projectFailure.Diagnostic == nil || projectFailure.Diagnostic.Reason != "workspace_changed_during_reconciliation" {
		t.Fatalf("reconcile accepted changed workspace: err=%v", err)
	}
	if err := os.Remove(filepath.Join(workspace.Path, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(workspace.Path+".before-reconcile", filepath.Join(workspace.Path, ".git")); err != nil {
		t.Fatal(err)
	}
}
