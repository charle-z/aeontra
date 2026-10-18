package edgeclient

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectAssociationPreviewAndApplyPreserveLegacyCheckoutInPlace(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	legacy := filepath.Join(roots.Dev, "old-project-name")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacy, "preserve.txt")
	if err := os.WriteFile(marker, []byte("preserved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaces, err := OpenWorkspaceRegistryWithRoots(state, roots)
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{legacy: ProjectCheckoutReady}}
	projects, err := OpenProjectRegistry(ProjectRegistryConfig{
		StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer projects.Close()
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	config := ProjectAssociationConfig{
		Projects: projects, Workspaces: workspaces, Roots: roots, Inspector: inspector,
		Now: func() time.Time { return now },
	}
	request := ProjectAssociationRequest{
		Alias: "Project", Owner: "charle-z", Repository: "repo", TargetAlias: "parrot",
		Profile: WorkspaceProfileLinuxWorkcell,
	}
	plan, err := PlanProjectAssociation(context.Background(), config, request)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ProjectAssociationAssociateExisting || plan.CandidatePath != legacy {
		t.Fatalf("plan=%+v", plan)
	}
	encoded, err := json.Marshal(plan.SafePreview())
	if err != nil {
		t.Fatal(err)
	}
	preview := string(encoded)
	for _, forbidden := range []string{legacy, roots.Dev, state, "candidate_path", "workspace_id"} {
		if strings.Contains(preview, forbidden) {
			t.Fatalf("safe association preview exposed %q: %s", forbidden, preview)
		}
	}
	result, err := ApplyProjectAssociation(context.Background(), config, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Alias != "project" || result.Repository != "charle-z/repo" || result.Target != "parrot" || result.State != "ready" {
		t.Fatalf("result=%+v", result)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "preserved\n" {
		t.Fatalf("association mutated checkout: %q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(roots.Dev, "repo")); !os.IsNotExist(err) {
		t.Fatalf("association moved checkout to canonical path: %v", err)
	}
	items, err := workspaces.List()
	if err != nil || len(items) != 1 || items[0].Path != legacy {
		t.Fatalf("workspaces=%+v err=%v", items, err)
	}
}

func TestProjectAssociationApplyRejectsExpiredOrChangedPlanWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*testing.T, WorkspaceRoots, map[string]ProjectCheckoutState)
		advance   time.Duration
		wantError ProjectErrorCode
	}{
		{
			name:      "expired",
			advance:   10 * time.Minute,
			wantError: ProjectErrorPlanExpired,
		},
		{
			name: "ambiguous after preview",
			mutate: func(t *testing.T, roots WorkspaceRoots, states map[string]ProjectCheckoutState) {
				second := filepath.Join(roots.Dev, "second-match")
				if err := os.Mkdir(second, 0o700); err != nil {
					t.Fatal(err)
				}
				states[second] = ProjectCheckoutReady
			},
			wantError: ProjectErrorPlanChanged,
		},
		{
			name: "dirty after preview",
			mutate: func(_ *testing.T, _ WorkspaceRoots, states map[string]ProjectCheckoutState) {
				for path := range states {
					states[path] = ProjectCheckoutDirty
				}
			},
			wantError: ProjectErrorPlanChanged,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			roots := newProjectDiscoveryRoots(t)
			legacy := filepath.Join(roots.Dev, "legacy")
			if err := os.Mkdir(legacy, 0o700); err != nil {
				t.Fatal(err)
			}
			states := map[string]ProjectCheckoutState{legacy: ProjectCheckoutReady}
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
			now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
			config := ProjectAssociationConfig{
				Projects: projects, Workspaces: workspaces, Roots: roots, Inspector: inspector,
				Now: func() time.Time { return now },
			}
			plan, err := PlanProjectAssociation(context.Background(), config, ProjectAssociationRequest{
				Alias: "project", Owner: "charle-z", Repository: "repo", TargetAlias: "parrot",
				Profile: WorkspaceProfileLinuxWorkcell,
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(t, roots, states)
			}
			now = now.Add(test.advance)
			if _, err := ApplyProjectAssociation(context.Background(), config, plan); !projectErrorIs(err, test.wantError) {
				t.Fatalf("apply err=%v", err)
			}
			items, err := workspaces.List()
			if err != nil || len(items) != 0 {
				t.Fatalf("failed apply mutated workspaces=%+v err=%v", items, err)
			}
		})
	}
}

func TestProjectAssociationPreviewBlocksDirtyAmbiguousAndMissingCheckouts(t *testing.T) {
	for _, test := range []struct {
		name      string
		states    map[string]ProjectCheckoutState
		wantError ProjectErrorCode
	}{
		{name: "dirty", states: map[string]ProjectCheckoutState{"legacy": ProjectCheckoutDirty}, wantError: ProjectErrorCheckoutDirty},
		{name: "ambiguous", states: map[string]ProjectCheckoutState{"a": ProjectCheckoutReady, "b": ProjectCheckoutReady}, wantError: ProjectErrorAmbiguousCheckout},
		{name: "missing", states: map[string]ProjectCheckoutState{}, wantError: ProjectErrorCheckoutMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			roots := newProjectDiscoveryRoots(t)
			mapped := make(map[string]ProjectCheckoutState, len(test.states))
			for name, checkoutState := range test.states {
				path := filepath.Join(roots.Dev, name)
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				mapped[path] = checkoutState
			}
			inspector := pathProjectInspector{states: mapped}
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
			_, err = PlanProjectAssociation(context.Background(), ProjectAssociationConfig{
				Projects: projects, Workspaces: workspaces, Roots: roots, Inspector: inspector,
			}, ProjectAssociationRequest{
				Alias: "project", Owner: "charle-z", Repository: "repo", TargetAlias: "parrot",
				Profile: WorkspaceProfileLinuxWorkcell,
			})
			if !projectErrorIs(err, test.wantError) {
				t.Fatalf("preview err=%v", err)
			}
		})
	}
}
