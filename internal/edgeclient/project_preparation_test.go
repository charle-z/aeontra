package edgeclient

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type projectPreparationRunner struct {
	calls      int
	dir        string
	args       []string
	credential GitHubCredential
	err        error
	after      func(string)
}

func (runner *projectPreparationRunner) Run(_ context.Context, dir string, args []string, credential GitHubCredential) (string, error) {
	runner.calls++
	runner.dir = dir
	runner.args = append([]string(nil), args...)
	runner.credential = credential
	if runner.after != nil {
		runner.after(dir)
	}
	return "", runner.err
}

func TestProjectPreparationCloneUsesClosedGitAuthorityAndRegistersAlias(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	candidate := filepath.Join(roots.Dev, "repo")
	states := map[string]ProjectCheckoutState{}
	inspector := pathProjectInspector{states: states}
	workspaces, projects := openProjectPreparationRegistries(t, state, roots, inspector)
	credential := GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)}
	runner := &projectPreparationRunner{after: func(dir string) {
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		states[dir] = ProjectCheckoutReady
	}}
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	config := ProjectPreparationConfig{
		StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
		Credential: credential, Runner: runner, Now: func() time.Time { return now },
	}
	plan, err := PlanProjectPreparation(context.Background(), config, ProjectPreparationRequest{
		Alias: "Project", Repository: "Repo", TargetAlias: "Parrot", Profile: WorkspaceProfileLinuxWorkcell,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != ProjectPreparationClone || plan.CandidatePath != candidate || plan.Owner != "charle-z" {
		t.Fatalf("plan=%+v", plan)
	}
	encoded, err := json.Marshal(plan.SafePreview())
	if err != nil {
		t.Fatal(err)
	}
	preview := string(encoded)
	for _, forbidden := range []string{candidate, roots.Dev, state, credential.Token, "workspace_id", "device_id", "candidate_path"} {
		if strings.Contains(preview, forbidden) {
			t.Fatalf("safe preview exposed %q: %s", forbidden, preview)
		}
	}
	status, err := ApplyProjectPreparation(context.Background(), config, plan)
	if err != nil {
		t.Fatal(err)
	}
	if status.Alias != "project" || status.Repository != "charle-z/repo" || status.Target != "parrot" || status.State != "ready" {
		t.Fatalf("status=%+v", status)
	}
	if runner.calls != 1 || runner.dir != candidate {
		t.Fatalf("runner=%+v", runner)
	}
	wantArgs := []string{"clone", "--single-branch", "--", "https://github.com/charle-z/repo.git", candidate}
	if !reflect.DeepEqual(runner.args, wantArgs) {
		t.Fatalf("args=%q want=%q", runner.args, wantArgs)
	}
	if runner.credential.Owner != credential.Owner || runner.credential.Token != credential.Token {
		t.Fatal("clone did not use the configured local GitHub authority")
	}
	items, err := workspaces.List()
	if err != nil || len(items) != 1 || items[0].Path != candidate || items[0].Profile != WorkspaceProfileLinuxWorkcell {
		t.Fatalf("workspaces=%+v err=%v", items, err)
	}
	resolved, err := projects.Resolve(context.Background(), "project", "parrot")
	if err != nil || resolved.Workspace.Path != candidate {
		t.Fatalf("resolved=%+v err=%v", resolved, err)
	}
}

func TestProjectPreparationAssociatesExistingCheckoutWithoutGit(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	legacy := filepath.Join(roots.Dev, "legacy-name")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(legacy, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{legacy: ProjectCheckoutReady}}
	workspaces, projects := openProjectPreparationRegistries(t, state, roots, inspector)
	runner := &projectPreparationRunner{}
	config := ProjectPreparationConfig{
		StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)}, Runner: runner,
	}
	plan, err := PlanProjectPreparation(context.Background(), config, ProjectPreparationRequest{
		Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: WorkspaceProfileLinuxWorkcell,
	})
	if err != nil || plan.Action != ProjectPreparationAssociateExisting || plan.CandidatePath != legacy {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	status, err := ApplyProjectPreparation(context.Background(), config, plan)
	if err != nil || status.State != "ready" || runner.calls != 0 {
		t.Fatalf("status=%+v calls=%d err=%v", status, runner.calls, err)
	}
	if _, err := os.Lstat(filepath.Join(roots.Dev, "repo")); !os.IsNotExist(err) {
		t.Fatalf("association created canonical clone: %v", err)
	}
}

func TestProjectPreparationUsesRegistryBeforeDiscoveryForClaimedRepository(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	claimedPath := filepath.Join(roots.Dev, "legacy-name")
	if err := os.Mkdir(claimedPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(claimedPath, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{claimedPath: ProjectCheckoutReady}}
	workspaces, projects := openProjectPreparationRegistries(t, state, roots, inspector)
	workspace, _, err := workspaces.AddProfile(claimedPath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := projects.Register(ProjectRegistration{
		Alias: "owner", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot",
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	config := ProjectPreparationConfig{
		StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)},
		Runner:     &projectPreparationRunner{},
	}
	_, err = PlanProjectPreparation(context.Background(), config, ProjectPreparationRequest{
		Alias: "ghost", Repository: "repo", TargetAlias: "parrot", Profile: WorkspaceProfileLinuxWorkcell,
	})
	var projectFailure *ProjectError
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorRepositoryConflict || projectFailure.Claim == nil || projectFailure.Claim.Alias != "owner" {
		t.Fatalf("phantom repository conflict err=%v", err)
	}
}

func TestProjectPreparationCloneFailureCleansOnlyReservedDirectory(t *testing.T) {
	for _, test := range []struct {
		name       string
		replaceDir bool
		wantRemain bool
	}{
		{name: "ordinary failure removes reserved directory"},
		{name: "replaced directory is preserved for review", replaceDir: true, wantRemain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			roots := newProjectDiscoveryRoots(t)
			candidate := filepath.Join(roots.Dev, "repo")
			inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{}}
			workspaces, projects := openProjectPreparationRegistries(t, state, roots, inspector)
			runner := &projectPreparationRunner{err: errors.New("clone failed"), after: func(dir string) {
				if !test.replaceDir {
					_ = os.WriteFile(filepath.Join(dir, "partial"), []byte("partial"), 0o600)
					return
				}
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "foreign"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}}
			config := ProjectPreparationConfig{
				StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
				Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)}, Runner: runner,
			}
			plan, err := PlanProjectPreparation(context.Background(), config, ProjectPreparationRequest{
				Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: WorkspaceProfileLinuxWorkcell,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = ApplyProjectPreparation(context.Background(), config, plan)
			wantCode := ProjectErrorCloneFailed
			if test.replaceDir {
				wantCode = ProjectErrorCleanupRequired
			}
			if !projectErrorIs(err, wantCode) {
				t.Fatalf("err=%v want=%s", err, wantCode)
			}
			_, statErr := os.Lstat(candidate)
			if test.wantRemain && statErr != nil {
				t.Fatalf("replacement was removed: %v", statErr)
			}
			if !test.wantRemain && !os.IsNotExist(statErr) {
				t.Fatalf("reserved directory remained: %v", statErr)
			}
		})
	}
}

func TestProjectPreparationReportsRegistrationStageAndCleansClone(t *testing.T) {
	for _, test := range []struct {
		name      string
		close     func(*WorkspaceRegistry, *ProjectRegistry) error
		wantError ProjectErrorCode
	}{
		{
			name: "workspace registry write",
			close: func(workspaces *WorkspaceRegistry, _ *ProjectRegistry) error {
				return workspaces.Close()
			},
			wantError: ProjectErrorWorkspaceLookup,
		},
		{
			name: "project registry write",
			close: func(_ *WorkspaceRegistry, projects *ProjectRegistry) error {
				return projects.Close()
			},
			// Registry-first preparation fails before cloning when the durable
			// registry is unavailable; no later registration stage is reachable.
			wantError: ProjectErrorRegistryUnavailable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			roots := newProjectDiscoveryRoots(t)
			candidate := filepath.Join(roots.Dev, "repo")
			states := map[string]ProjectCheckoutState{}
			workspaces, projects := openProjectPreparationRegistries(t, state, roots, pathProjectInspector{states: states})
			runner := &projectPreparationRunner{after: func(dir string) {
				if err := os.Mkdir(filepath.Join(dir, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				states[dir] = ProjectCheckoutReady
			}}
			config := ProjectPreparationConfig{
				StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
				Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)}, Runner: runner,
			}
			plan, err := PlanProjectPreparation(context.Background(), config, ProjectPreparationRequest{
				Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: WorkspaceProfileLinuxWorkcell,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := test.close(workspaces, projects); err != nil {
				t.Fatal(err)
			}
			if _, err := ApplyProjectPreparation(context.Background(), config, plan); !projectErrorIs(err, test.wantError) {
				t.Fatalf("err=%v want=%s", err, test.wantError)
			}
			if _, err := os.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed clone was not removed: %v", err)
			}
		})
	}
}

func TestProjectRegistrationFailureCodePreservesActionableProjectErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want ProjectErrorCode
	}{
		{name: "checkout", err: projectErr(ProjectErrorCheckoutUnsafe, errors.New("unsafe")), want: ProjectErrorCheckoutUnsafe},
		{name: "registry", err: projectErr(ProjectErrorRegistryUnavailable, errors.New("closed")), want: ProjectErrorProjectRegistration},
		{name: "unclassified", err: errors.New("failed"), want: ProjectErrorProjectRegistration},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := projectRegistrationFailureCode(test.err); got != test.want {
				t.Fatalf("got=%s want=%s", got, test.want)
			}
		})
	}
}

func TestWorkspaceRegistrationFailureCodeReportsSafeStage(t *testing.T) {
	for _, test := range []struct {
		name  string
		stage workspaceRegistrationStage
		want  ProjectErrorCode
	}{
		{name: "validation", stage: workspaceRegistrationValidation, want: ProjectErrorWorkspaceValidation},
		{name: "lookup", stage: workspaceRegistrationLookup, want: ProjectErrorWorkspaceLookup},
		{name: "identity", stage: workspaceRegistrationIdentity, want: ProjectErrorWorkspaceRegistration},
		{name: "write", stage: workspaceRegistrationWrite, want: ProjectErrorWorkspaceWrite},
		{name: "profile", stage: workspaceRegistrationProfile, want: ProjectErrorProfileDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := workspaceRegistrationErr(test.stage, errors.New("failed"))
			if got := workspaceRegistrationFailureCode(err); got != test.want {
				t.Fatalf("got=%s want=%s", got, test.want)
			}
		})
	}
}

func TestProjectPreparationRejectsExpiredChangedAndMissingAuthority(t *testing.T) {
	state := t.TempDir()
	roots := newProjectDiscoveryRoots(t)
	inspector := pathProjectInspector{states: map[string]ProjectCheckoutState{}}
	workspaces, projects := openProjectPreparationRegistries(t, state, roots, inspector)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	base := ProjectPreparationConfig{
		StateRoot: state, Projects: projects, Workspaces: workspaces, Roots: roots,
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: strings.Repeat("t", 32)},
		Runner:     &projectPreparationRunner{}, Now: func() time.Time { return now },
	}
	request := ProjectPreparationRequest{Alias: "project", Repository: "repo", TargetAlias: "parrot", Profile: WorkspaceProfileLinuxWorkcell}
	plan, err := PlanProjectPreparation(context.Background(), base, request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if _, err := ApplyProjectPreparation(context.Background(), base, plan); !projectErrorIs(err, ProjectErrorPlanExpired) {
		t.Fatalf("expired err=%v", err)
	}
	now = plan.CreatedAt
	changed := plan
	changed.Repository = "other"
	if _, err := ApplyProjectPreparation(context.Background(), base, changed); !projectErrorIs(err, ProjectErrorPlanChanged) {
		t.Fatalf("changed err=%v", err)
	}
	missing := base
	missing.Credential = GitHubCredential{}
	if _, err := PlanProjectPreparation(context.Background(), missing, request); !projectErrorIs(err, ProjectErrorCredentialUnavailable) {
		t.Fatalf("missing authority err=%v", err)
	}
}

func openProjectPreparationRegistries(t *testing.T, state string, roots WorkspaceRoots, inspector ProjectCheckoutInspector) (*WorkspaceRegistry, *ProjectRegistry) {
	t.Helper()
	workspaces, err := OpenWorkspaceRegistryWithRoots(state, roots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspaces.Close() })
	projects, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: inspector})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = projects.Close() })
	return workspaces, projects
}
