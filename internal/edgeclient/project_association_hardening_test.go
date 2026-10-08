package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectAssociationRejectsFutureAndBoundaryExpiredPlans(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*ProjectAssociationPlan, *time.Time)
		wantError ProjectErrorCode
	}{
		{
			name: "future",
			mutate: func(plan *ProjectAssociationPlan, _ *time.Time) {
				plan.CreatedAt = plan.CreatedAt.Add(time.Hour)
				plan.ExpiresAt = plan.ExpiresAt.Add(time.Hour)
			},
			wantError: ProjectErrorPlanChanged,
		},
		{
			name: "exact expiry",
			mutate: func(plan *ProjectAssociationPlan, now *time.Time) {
				*now = plan.ExpiresAt
			},
			wantError: ProjectErrorPlanExpired,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, request, now := newProjectAssociationHardeningFixture(t, ProjectCheckoutReady)
			plan, err := PlanProjectAssociation(context.Background(), config, request)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&plan, now)
			if _, err := ApplyProjectAssociation(context.Background(), config, plan); !projectErrorIs(err, test.wantError) {
				t.Fatalf("plan validation err=%v", err)
			}
		})
	}
}

func TestProjectAssociationCannotOverrideRegistryCheckoutInspector(t *testing.T) {
	config, request, _ := newProjectAssociationHardeningFixture(t, ProjectCheckoutDirty)
	config.Inspector = pathProjectInspector{states: map[string]ProjectCheckoutState{
		filepath.Join(config.Roots.Dev, "legacy"): ProjectCheckoutReady,
	}}
	if _, err := PlanProjectAssociation(context.Background(), config, request); !projectErrorIs(err, ProjectErrorCheckoutDirty) {
		t.Fatalf("weaker association inspector overrode registry inspector: %v", err)
	}
}

func TestProjectAssociationRetryConvergesAfterWorkspaceRegistrationOnly(t *testing.T) {
	config, request, _ := newProjectAssociationHardeningFixture(t, ProjectCheckoutReady)
	plan, err := PlanProjectAssociation(context.Background(), config, request)
	if err != nil {
		t.Fatal(err)
	}
	workspace, created, err := config.Workspaces.AddProfile(plan.CandidatePath, plan.Profile)
	if err != nil || !created {
		t.Fatalf("simulate interrupted workspace registration created=%v err=%v", created, err)
	}
	status, err := ApplyProjectAssociation(context.Background(), config, plan)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "ready" || status.Alias != "project" {
		t.Fatalf("status=%+v", status)
	}
	items, err := config.Workspaces.List()
	if err != nil || len(items) != 1 || items[0].ID != workspace.ID {
		t.Fatalf("retry duplicated workspace: %+v err=%v", items, err)
	}
}

func newProjectAssociationHardeningFixture(t *testing.T, checkoutState ProjectCheckoutState) (ProjectAssociationConfig, ProjectAssociationRequest, *time.Time) {
	t.Helper()
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	legacy := filepath.Join(roots.Dev, "legacy")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(legacy, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{legacy: checkoutState}}
	workspaces, err := OpenWorkspaceRegistryWithRoots(state, roots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaces.Close() })
	projects, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = projects.Close() })
	now := time.Date(2026, 7, 23, 1, 0, 0, 0, time.UTC)
	return ProjectAssociationConfig{
			Projects: projects, Workspaces: workspaces, Roots: roots, Inspector: inspector,
			Now: func() time.Time { return now },
		}, ProjectAssociationRequest{
			Alias: "project", Owner: "charle-z", Repository: "repo", TargetAlias: "parrot",
			Profile: WorkspaceProfileLinuxWorkcell,
		}, &now
}
