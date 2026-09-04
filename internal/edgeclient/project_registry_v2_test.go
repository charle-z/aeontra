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

type detailedProjectInspector struct {
	observation ProjectCheckoutObservation
	err         error
	calls       int
}

type mutatingRepositoryInspector struct {
	remoteMatches bool
}

func (f *mutatingRepositoryInspector) observation(path, owner, repository string) ProjectCheckoutObservation {
	if f.remoteMatches {
		return ProjectCheckoutObservation{State: ProjectCheckoutReady, Diagnostic: ProjectCheckoutDiagnostic{Reason: "checkout_ready"}}
	}
	return repositoryMismatchObservation(path, owner, repository, "https://github.com/other/repository.git")
}

func (f *mutatingRepositoryInspector) Inspect(_ context.Context, path, owner, repository string) (ProjectCheckoutState, error) {
	observation := f.observation(path, owner, repository)
	if observation.State == ProjectCheckoutIdentityMismatch {
		return ProjectCheckoutRemoteMismatch, nil
	}
	return observation.State, nil
}

func (f *mutatingRepositoryInspector) InspectDetailed(_ context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	return f.observation(path, owner, repository), nil
}

func (f *mutatingRepositoryInspector) InspectRepositoryIdentity(_ context.Context, path, owner, repository string) (ProjectCheckoutObservation, error) {
	return f.observation(path, owner, repository), nil
}

func (f *detailedProjectInspector) Inspect(context.Context, string, string, string) (ProjectCheckoutState, error) {
	if f.err != nil {
		return ProjectCheckoutUnavailable, f.err
	}
	return f.observation.State, nil
}

func (f *detailedProjectInspector) InspectDetailed(context.Context, string, string, string) (ProjectCheckoutObservation, error) {
	f.calls++
	if f.err != nil {
		return ProjectCheckoutObservation{}, f.err
	}
	return f.observation, nil
}

func TestProjectRegistryDetailedCheckoutFailureIsNotUnsafeCatchAll(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	detailed := &detailedProjectInspector{observation: ProjectCheckoutObservation{State: ProjectCheckoutReady}}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: detailed})
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
	detailed.observation = ProjectCheckoutObservation{
		State:      ProjectCheckoutTimeout,
		Diagnostic: ProjectCheckoutDiagnostic{Reason: "git_status_timeout", Repairable: true, RecommendedAction: "project_reconcile"},
	}
	status, err := registry.Status(context.Background(), "project", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	if status.Reason != ProjectErrorCheckoutTimeout || status.Diagnostic == nil || status.Diagnostic.Reason != "git_status_timeout" {
		t.Fatalf("status=%+v", status)
	}
	var projectFailure *ProjectError
	_, err = registry.Resolve(context.Background(), "project", "parrot")
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorCheckoutTimeout || projectFailure.Diagnostic == nil {
		t.Fatalf("resolve err=%v", err)
	}
}

func TestProjectRegistryListClaimsDetectsRemoteConfigMutation(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	inspector := &mutatingRepositoryInspector{remoteMatches: true}
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
	// Mutating origin changes Git repository identity but not the filesystem
	// inode-based attestation. The bounded identity inspector must catch it.
	inspector.remoteMatches = false
	claims, err = registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimStale || claims[0].Reason != ProjectErrorRepositoryMismatch || !claims[0].Repairable {
		t.Fatalf("remote drift was reported healthy: claims=%+v err=%v", claims, err)
	}
	status, err := registry.Status(context.Background(), "project", "parrot")
	if err != nil || status.Reason != ProjectErrorRepositoryMismatch {
		t.Fatalf("status did not agree with registry list: status=%+v err=%v", status, err)
	}
}

func TestProjectAttestationIgnoresUntrackedWorkspaceFiles(t *testing.T) {
	_, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	before, err := ProjectAttestationFingerprint(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, "build-output.tmp"), []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := ProjectAttestationFingerprint(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("untracked file changed attestation: before=%s after=%s", before, after)
	}
	if _, err := workspaces.Get(workspace.ID); err != nil {
		t.Fatalf("workspace became unavailable after ordinary mutation: %v", err)
	}
}

func TestProjectRegistryFailsClosedWhenNewClaimCannotBeAttested(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if err := os.Remove(filepath.Join(workspace.Path, ".git")); err != nil {
		t.Fatal(err)
	}
	_, _, err = registry.Register(ProjectRegistration{
		Alias: "project", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot",
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	})
	var projectFailure *ProjectError
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorCheckoutUnavailable || projectFailure.Diagnostic == nil || projectFailure.Diagnostic.Reason != "workspace_attestation_unavailable" {
		t.Fatalf("registration error=%v", err)
	}
}

func TestProjectRegistryResolveRegisteredSkipsGitInspection(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	detailed := &detailedProjectInspector{observation: ProjectCheckoutObservation{State: ProjectCheckoutReady}}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: detailed})
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
	detailed.calls = 0
	detailed.observation = ProjectCheckoutObservation{State: ProjectCheckoutTimeout, Diagnostic: ProjectCheckoutDiagnostic{Reason: "git_status_timeout"}}
	resolution, err := registry.ResolveRegistered("project", "parrot")
	if err != nil || resolution.CheckoutState != ProjectCheckoutRegistered || !resolution.RegisteredOnly {
		t.Fatalf("resolution=%+v err=%v", resolution, err)
	}
	if detailed.calls != 0 {
		t.Fatalf("registered resolution called inspector %d times", detailed.calls)
	}
}

func TestProjectRegistryResolveRegisteredRejectsLegacyWithoutAttestation(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	detailed := &detailedProjectInspector{observation: ProjectCheckoutObservation{State: ProjectCheckoutReady}}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: detailed})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	if _, _, err := registry.Register(ProjectRegistration{
		Alias: "legacy", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot",
		WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.db.Exec(`UPDATE projects SET attestation_fingerprint='' WHERE alias='legacy'`); err != nil {
		t.Fatal(err)
	}
	detailed.calls = 0
	resolution, err := registry.ResolveRegistered("legacy", "parrot")
	if resolution.Project.Alias != "" {
		t.Fatalf("legacy resolution unexpectedly returned project: %+v", resolution)
	}
	var projectFailure *ProjectError
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorCheckoutMissing || projectFailure.Diagnostic == nil || projectFailure.Diagnostic.Reason != "workspace_attestation_missing" || !projectFailure.Diagnostic.Repairable || projectFailure.Diagnostic.RecommendedAction != "project_reconcile" {
		t.Fatalf("legacy resolution error=%v", err)
	}
	if detailed.calls != 0 {
		t.Fatalf("legacy resolution ran the Git inspector %d times", detailed.calls)
	}
}

func TestProjectRegistryListsAndReleasesStaleClaimWithoutWorkspaceDeletion(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	if err := workspaces.Remove(workspace.ID); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ReconcileClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimStale || claims[0].Reason != ProjectErrorWorkspaceMissing || !claims[0].Repairable {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	if err := registry.ReleaseClaim("project", "charle-z", "repo", "parrot", 1); err != nil {
		t.Fatal(err)
	}
	claims, err = registry.ListClaims()
	if err != nil || len(claims) != 0 {
		t.Fatalf("claims after release=%+v err=%v", claims, err)
	}
	var profileRows int
	if err := registry.db.QueryRow(`SELECT COUNT(*) FROM project_profiles WHERE alias='project'`).Scan(&profileRows); err != nil {
		t.Fatal(err)
	}
	if profileRows != 0 {
		t.Fatalf("release left project profile metadata behind: %d rows", profileRows)
	}
}

func TestProjectRegistryMarksRepositorySwapAsStaleClaim(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	if err := os.Rename(filepath.Join(workspace.Path, ".git"), filepath.Join(workspace.Path, ".git-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(workspace.Path, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimStale || claims[0].Reason != ProjectErrorCheckoutIdentityMismatch || !claims[0].Repairable {
		t.Fatalf("swapped claims=%+v err=%v", claims, err)
	}
	if err := registry.ReleaseClaim("project", "charle-z", "repo", "parrot", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace.Path); err != nil {
		t.Fatalf("release removed workspace: %v", err)
	}
}

func TestProjectRegistryRefusesToReleaseHealthyClaim(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	if err := registry.ReleaseClaim("project", "charle-z", "repo", "parrot", 1); ProjectErrorCodeOf(err) != ProjectErrorClaimHealthy {
		t.Fatalf("healthy release error=%v", err)
	}
}

func TestProjectRegistryReleaseIsScopedToOneTargetBinding(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	secondPath := filepath.Join(filepath.Dir(workspace.Path), "repo-second")
	if err := os.MkdirAll(filepath.Join(secondPath, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	second, _, err := workspaces.AddProfile(secondPath, WorkspaceProfileLinuxWorkcell)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixNano()
	if _, err := registry.db.Exec(`INSERT INTO project_workspaces(alias,target_alias,workspace_id,created_at,updated_at) VALUES(?,?,?,?,?)`, "project", "windows", second.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if err := workspaces.Remove(workspace.ID); err != nil {
		t.Fatal(err)
	}
	if err := registry.ReleaseClaim("project", "charle-z", "repo", "parrot", 1); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].Target != "windows" || claims[0].WorkspaceID != second.ID {
		t.Fatalf("claims after scoped release=%+v err=%v", claims, err)
	}
	var preferred string
	if err := registry.db.QueryRow(`SELECT preferred_target FROM projects WHERE alias='project'`).Scan(&preferred); err != nil || preferred != "windows" {
		t.Fatalf("preferred target=%q err=%v", preferred, err)
	}
}

func TestProjectRegistryMigratesV1ClaimsWithoutDroppingRows(t *testing.T) {
	state, workspaces, _ := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	databasePath := filepath.Join(state, projectRegistryFile)
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE projects (alias TEXT PRIMARY KEY, owner TEXT NOT NULL, repository TEXT NOT NULL, preferred_target TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, UNIQUE(owner,repository))`,
		`CREATE TABLE project_profiles (alias TEXT NOT NULL, profile TEXT NOT NULL, PRIMARY KEY(alias,profile))`,
		`CREATE TABLE project_workspaces (alias TEXT NOT NULL, target_alias TEXT NOT NULL, workspace_id TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(alias,target_alias))`,
		`PRAGMA user_version=1`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO projects(alias,owner,repository,preferred_target,created_at,updated_at) VALUES('legacy','charle-z','legacy','parrot',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	var version int
	if err := registry.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	project, found, err := loadProject(registry.db, "legacy")
	if err != nil || !found || project.Alias != "legacy" || project.ClaimGeneration != 1 {
		t.Fatalf("project=%+v found=%v err=%v", project, found, err)
	}
}

func TestProjectRegistryMigratesSchemaZeroLegacyClaimAsRepairable(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	databasePath := filepath.Join(state, projectRegistryFile)
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE projects (alias TEXT PRIMARY KEY, owner TEXT NOT NULL, repository TEXT NOT NULL, preferred_target TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, UNIQUE(owner,repository))`,
		`CREATE TABLE project_profiles (alias TEXT NOT NULL, profile TEXT NOT NULL, PRIMARY KEY(alias,profile))`,
		`CREATE TABLE project_workspaces (alias TEXT NOT NULL, target_alias TEXT NOT NULL, workspace_id TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY(alias,target_alias))`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// Leave PRAGMA user_version at SQLite's default zero. This models a
	// legacy database created before the schema version was recorded.
	if _, err := db.Exec(`INSERT INTO projects(alias,owner,repository,preferred_target,created_at,updated_at) VALUES('legacy-zero','charle-z','legacy-zero','parrot',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project_profiles(alias,profile) VALUES('legacy-zero','linux-workcell')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project_workspaces(alias,target_alias,workspace_id,created_at,updated_at) VALUES(?,?,?,?,?)`, "legacy-zero", "parrot", workspace.ID, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project_profiles(alias,profile) VALUES('orphan-profile','linux-workcell')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO project_workspaces(alias,target_alias,workspace_id,created_at,updated_at) VALUES(?,?,?,?,?)`, "orphan-workspace", "parrot", "ws-orphan", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	var version int
	if err := registry.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	claims, err := registry.ListClaims()
	if err != nil || len(claims) != 1 {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	claim := claims[0]
	if claim.Alias != "legacy-zero" || claim.Target != "parrot" || claim.WorkspaceID != workspace.ID || claim.Generation != 1 || claim.State != ProjectClaimRepairable || claim.Reason != ProjectErrorCheckoutMissing || !claim.Repairable {
		t.Fatalf("legacy schema-zero claim was adopted: %+v", claim)
	}
	var attestation string
	if err := registry.db.QueryRow(`SELECT attestation_fingerprint FROM projects WHERE alias='legacy-zero'`).Scan(&attestation); err != nil {
		t.Fatal(err)
	}
	if attestation != "" {
		t.Fatalf("schema-zero migration unexpectedly adopted attestation=%q", attestation)
	}
	var orphanProfiles, orphanWorkspaces int
	if err := registry.db.QueryRow(`SELECT COUNT(*) FROM project_profiles WHERE alias='orphan-profile'`).Scan(&orphanProfiles); err != nil {
		t.Fatal(err)
	}
	if err := registry.db.QueryRow(`SELECT COUNT(*) FROM project_workspaces WHERE alias='orphan-workspace'`).Scan(&orphanWorkspaces); err != nil {
		t.Fatal(err)
	}
	if orphanProfiles != 0 || orphanWorkspaces != 0 {
		t.Fatalf("legacy orphan metadata survived migration: profiles=%d workspaces=%d", orphanProfiles, orphanWorkspaces)
	}
}

func TestProjectRegistryRejectsCorruptClaimGeneration(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	if _, err := registry.db.Exec(`UPDATE projects SET claim_generation=-1 WHERE alias='project'`); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaims()
	if err != nil || len(claims) != 1 || claims[0].State != ProjectClaimRepairable || claims[0].Reason != ProjectErrorPlanChanged || !claims[0].Repairable {
		t.Fatalf("corrupt generation was not repairable: claims=%+v err=%v", claims, err)
	}
	if _, err := registry.ResolveRegistered("project", "parrot"); ProjectErrorCodeOf(err) != ProjectErrorPlanChanged {
		t.Fatalf("resolve accepted corrupt generation: %v", err)
	}
	if _, err := registry.ReconcileClaim(context.Background(), "project", "parrot"); ProjectErrorCodeOf(err) != ProjectErrorPlanChanged {
		t.Fatalf("reconcile accepted corrupt generation: %v", err)
	}
}

func TestProjectRegistryListClaimsFiltersForeignOwners(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
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
	if _, err := registry.db.Exec(`UPDATE projects SET owner='other-owner' WHERE alias='project'`); err != nil {
		t.Fatal(err)
	}
	claims, err := registry.ListClaims()
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 0 {
		t.Fatalf("foreign owner claim leaked through list: %+v", claims)
	}
}

func TestProjectRegistryRepositoryConflictIncludesClaimant(t *testing.T) {
	state, workspaces, workspace := newProjectRegistryFixture(t, WorkspaceProfileLinuxWorkcell)
	registry, err := OpenProjectRegistry(ProjectRegistryConfig{StateRoot: state, AllowedOwner: "charle-z", Workspaces: workspaces, Inspector: fixedProjectInspector{state: ProjectCheckoutReady}})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	base := ProjectRegistration{Alias: "owner", Owner: "charle-z", Repository: "repo", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID, AllowedProfiles: []WorkspaceProfile{WorkspaceProfileLinuxWorkcell}}
	if _, _, err := registry.Register(base); err != nil {
		t.Fatal(err)
	}
	base.Alias = "ghost"
	_, _, err = registry.Register(base)
	var projectFailure *ProjectError
	if !errors.As(err, &projectFailure) || projectFailure.Code != ProjectErrorRepositoryConflict || projectFailure.Claim == nil || projectFailure.Claim.Alias != "owner" {
		t.Fatalf("conflict err=%v", err)
	}
}

func TestPublicCheckoutObservedValueDoesNotLeakArbitraryRemoteData(t *testing.T) {
	if got := publicCheckoutObservedValue("https://token:secret@github.com/CHARLE-Z/Repo.git"); got != "https://github.com/charle-z/repo.git" {
		t.Fatalf("safe GitHub remote=%q", got)
	}
	for _, remote := range []string{
		"https://github.com/token/charle-z/repo.git",
		"https://github.com/charle-z/repo.git/extra",
		"https://github.com:8443/charle-z/repo.git",
		"https://github.com/charle-z/repo.git?redirect=https://secret.example",
	} {
		if got := publicCheckoutObservedValue(remote); got != "" {
			t.Fatalf("unsafe remote was exposed as %q: %s", got, remote)
		}
	}
}

func TestGitCommonDirectoryRejectsNonRegularMarker(t *testing.T) {
	gitDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(gitDir, "commondir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveGitCommonDirectory(gitDir); err == nil {
		t.Fatal("directory commondir marker was accepted")
	}
}
