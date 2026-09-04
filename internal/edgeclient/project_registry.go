package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	projectRegistryFile              = "projects.db"
	projectSchemaVersion             = 2
	maxProjectProfiles               = 4
	maxProjectClaims                 = 32
	maxProjectClaimGeneration uint64 = 1<<63 - 1
)

var (
	projectAliasPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	projectTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
)

type ProjectCheckoutState string

const (
	ProjectCheckoutReady            ProjectCheckoutState = "ready"
	ProjectCheckoutDirty            ProjectCheckoutState = "dirty"
	ProjectCheckoutRemoteMismatch   ProjectCheckoutState = "remote_mismatch"
	ProjectCheckoutRegistered       ProjectCheckoutState = "registered"
	ProjectCheckoutUnavailable      ProjectCheckoutState = "unavailable"
	ProjectCheckoutTimeout          ProjectCheckoutState = "timeout"
	ProjectCheckoutIdentityMismatch ProjectCheckoutState = "identity_mismatch"
	ProjectCheckoutCorrupt          ProjectCheckoutState = "corrupt"
	ProjectCheckoutUnsafeBoundary   ProjectCheckoutState = "unsafe_boundary"
	// ProjectCheckoutUnsafe is retained for inspectors implementing the v1
	// contract. New inspectors should return ProjectCheckoutUnsafeBoundary.
	ProjectCheckoutUnsafe ProjectCheckoutState = "unsafe"
)

// ProjectCheckoutDiagnostic is the bounded, actionable explanation for a
// checkout observation. Paths and values are retained for the Edge control
// plane; callers that expose this structure must apply their normal redaction
// policy before returning it to an untrusted client.
type ProjectCheckoutDiagnostic struct {
	Reason            string `json:"reason"`
	Path              string `json:"path,omitempty"`
	Expected          string `json:"expected,omitempty"`
	Observed          string `json:"observed,omitempty"`
	Repairable        bool   `json:"repairable"`
	RecommendedAction string `json:"recommended_action,omitempty"`
}

type ProjectCheckoutObservation struct {
	State      ProjectCheckoutState
	Diagnostic ProjectCheckoutDiagnostic
}

// ProjectCheckoutInspectorV2 is additive to ProjectCheckoutInspector. Keeping
// the v1 interface allows old Edge bundles and tests to continue operating
// while the registry can consume typed observations when available.
type ProjectCheckoutInspectorV2 interface {
	InspectDetailed(context.Context, string, string, string) (ProjectCheckoutObservation, error)
}

type ProjectErrorCode string

const (
	ProjectErrorInvalidInput             ProjectErrorCode = "invalid_input"
	ProjectErrorOwnerDenied              ProjectErrorCode = "owner_denied"
	ProjectErrorAliasConflict            ProjectErrorCode = "alias_conflict"
	ProjectErrorRepositoryConflict       ProjectErrorCode = "repository_conflict"
	ProjectErrorProfileDenied            ProjectErrorCode = "profile_denied"
	ProjectErrorProjectNotFound          ProjectErrorCode = "project_not_found"
	ProjectErrorTargetNotFound           ProjectErrorCode = "target_not_found"
	ProjectErrorWorkspaceMissing         ProjectErrorCode = "workspace_unavailable"
	ProjectErrorWorkspaceConflict        ProjectErrorCode = "workspace_conflict"
	ProjectErrorCheckoutDirty            ProjectErrorCode = "checkout_dirty"
	ProjectErrorRepositoryMismatch       ProjectErrorCode = "repository_mismatch"
	ProjectErrorCheckoutUnsafe           ProjectErrorCode = "checkout_unsafe"
	ProjectErrorCheckoutUnavailable      ProjectErrorCode = "checkout_unavailable"
	ProjectErrorCheckoutTimeout          ProjectErrorCode = "checkout_timeout"
	ProjectErrorCheckoutIdentityMismatch ProjectErrorCode = "checkout_identity_mismatch"
	ProjectErrorCheckoutCorrupt          ProjectErrorCode = "checkout_corrupt"
	ProjectErrorCheckoutUnsafeBoundary   ProjectErrorCode = "checkout_unsafe_boundary"
	ProjectErrorClaimHealthy             ProjectErrorCode = "claim_healthy"
	ProjectErrorCheckoutMissing          ProjectErrorCode = "checkout_missing"
	ProjectErrorAmbiguousCheckout        ProjectErrorCode = "ambiguous_checkout"
	ProjectErrorDiscoveryLimit           ProjectErrorCode = "discovery_limit"
	ProjectErrorDiscoveryTimeout         ProjectErrorCode = "discovery_timeout"
	ProjectErrorPlanChanged              ProjectErrorCode = "plan_changed"
	ProjectErrorPlanExpired              ProjectErrorCode = "plan_expired"
	ProjectErrorRegistryUnavailable      ProjectErrorCode = "registry_unavailable"
	ProjectErrorWorkspaceRegistration    ProjectErrorCode = "workspace_registration_failed"
	ProjectErrorWorkspaceValidation      ProjectErrorCode = "workspace_validation_failed"
	ProjectErrorWorkspaceLookup          ProjectErrorCode = "workspace_lookup_failed"
	ProjectErrorWorkspaceWrite           ProjectErrorCode = "workspace_write_failed"
	ProjectErrorProjectRegistration      ProjectErrorCode = "project_registration_failed"
	ProjectErrorCredentialUnavailable    ProjectErrorCode = "credential_unavailable"
	ProjectErrorCloneFailed              ProjectErrorCode = "clone_failed"
	ProjectErrorCleanupRequired          ProjectErrorCode = "cleanup_required"
)

type ProjectError struct {
	Code       ProjectErrorCode
	Err        error
	Diagnostic *ProjectCheckoutDiagnostic
	Claim      *ProjectClaim
}

func (e *ProjectError) Error() string {
	message := "project resolution failed: " + string(e.Code)
	if e.Diagnostic != nil && e.Diagnostic.Reason != "" {
		message += " reason=" + e.Diagnostic.Reason
	}
	if e.Claim != nil && e.Claim.Alias != "" {
		message += " claimant_alias=" + e.Claim.Alias
	}
	return message
}

func (e *ProjectError) Unwrap() error { return e.Err }

type Project struct {
	Alias           string `json:"alias"`
	Owner           string `json:"owner"`
	Repository      string `json:"repository"`
	PreferredTarget string `json:"preferred_target"`
	ClaimGeneration uint64 `json:"claim_generation"`
	// ClaimGenerationValid is populated only from a durable registry row. It
	// is intentionally not serialized: callers must not be able to bless a
	// malformed legacy value by constructing a Project literal.
	ClaimGenerationValid   bool      `json:"-"`
	AttestationFingerprint string    `json:"-"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ProjectRegistration struct {
	Alias           string
	Owner           string
	Repository      string
	PreferredTarget string
	TargetAlias     string
	WorkspaceID     string
	AllowedProfiles []WorkspaceProfile
}

type ProjectRegistryConfig struct {
	StateRoot    string
	AllowedOwner string
	Workspaces   *WorkspaceRegistry
	Inspector    ProjectCheckoutInspector
}

type ProjectCheckoutInspector interface {
	Inspect(context.Context, string, string, string) (ProjectCheckoutState, error)
}

// ProjectRepositoryIdentityInspector is the deliberately narrow read used by
// registry recovery. It verifies the workspace boundary and owner-bound Git
// remotes without running status, discovery, or arbitrary repository scans.
// A full checkout inspector may implement it in addition to the legacy
// interface; older test/custom inspectors remain compatible and are treated as
// lacking this optional observation.
type ProjectRepositoryIdentityInspector interface {
	InspectRepositoryIdentity(context.Context, string, string, string) (ProjectCheckoutObservation, error)
}

type ProjectRegistry struct {
	db           *sql.DB
	now          func() time.Time
	allowedOwner string
	workspaces   *WorkspaceRegistry
	inspector    ProjectCheckoutInspector
}

type ProjectResolution struct {
	Project            Project
	TargetAlias        string
	Workspace          Workspace
	CheckoutState      ProjectCheckoutState
	CheckoutDiagnostic *ProjectCheckoutDiagnostic
	RegisteredOnly     bool
}

type ProjectStatus struct {
	Alias      string                     `json:"alias"`
	Repository string                     `json:"repository"`
	Target     string                     `json:"target"`
	State      string                     `json:"state"`
	Profile    WorkspaceProfile           `json:"profile,omitempty"`
	Mode       WorkspaceMode              `json:"mode,omitempty"`
	Reason     ProjectErrorCode           `json:"reason,omitempty"`
	Diagnostic *ProjectCheckoutDiagnostic `json:"diagnostic,omitempty"`
}

type ProjectClaimState string

const (
	ProjectClaimHealthy    ProjectClaimState = "healthy"
	ProjectClaimStale      ProjectClaimState = "stale"
	ProjectClaimRepairable ProjectClaimState = "repairable"
)

// ProjectClaim is a registry-only view. It intentionally omits workspace
// paths and Git output, but makes repository ownership visible enough to
// diagnose alias/repository conflicts without guessing.
type ProjectClaim struct {
	Alias       string            `json:"alias"`
	Owner       string            `json:"owner"`
	Repository  string            `json:"repository"`
	Target      string            `json:"target"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Generation  uint64            `json:"generation"`
	State       ProjectClaimState `json:"state"`
	Reason      ProjectErrorCode  `json:"reason,omitempty"`
	Repairable  bool              `json:"repairable"`
}

func OpenProjectRegistry(config ProjectRegistryConfig) (*ProjectRegistry, error) {
	stateRoot := filepath.Clean(strings.TrimSpace(config.StateRoot))
	allowedOwner, _, err := NormalizeProjectRepository(config.AllowedOwner, "project")
	if err != nil || config.Workspaces == nil || config.Workspaces.db == nil {
		return nil, projectErr(ProjectErrorInvalidInput, errors.New("project registry configuration is invalid"))
	}
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	path := filepath.Join(stateRoot, projectRegistryFile)
	if err := prepareProjectRegistryFile(path); err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	db.SetMaxOpenConns(1)
	if err := initializeProjectRegistry(db); err != nil {
		_ = db.Close()
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if err := securePrivateRegularPath(path); err != nil {
		_ = db.Close()
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	inspector := config.Inspector
	if inspector == nil {
		inspector = newProjectCheckoutInspectorWithRoots(config.Workspaces.roots)
	}
	return &ProjectRegistry{
		db: db, now: time.Now, allowedOwner: allowedOwner,
		workspaces: config.Workspaces, inspector: inspector,
	}, nil
}

func initializeProjectRegistry(db *sql.DB) error {
	if db == nil {
		return errors.New("project registry database is unavailable")
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > projectSchemaVersion {
		return errors.New("project registry schema is newer than this Edge release")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA max_page_count=4096`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS projects (
			alias TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			repository TEXT NOT NULL,
			preferred_target TEXT NOT NULL,
			registration_state TEXT NOT NULL DEFAULT 'healthy',
			claim_generation INTEGER NOT NULL DEFAULT 1,
			attestation_fingerprint TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(owner,repository)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS project_profiles (
			alias TEXT NOT NULL REFERENCES projects(alias) ON DELETE CASCADE,
			profile TEXT NOT NULL,
			PRIMARY KEY(alias,profile)
		) WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS project_workspaces (
			alias TEXT NOT NULL REFERENCES projects(alias) ON DELETE CASCADE,
			target_alias TEXT NOT NULL,
			workspace_id TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(alias,target_alias)
		) WITHOUT ROWID`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if version <= 1 {
		// v1 rows, and schema-zero databases left by an interrupted legacy
		// initialization, are retained. Adding only missing columns avoids
		// stamping an old CREATE TABLE layout as v2 while it is still unusable.
		for name, statement := range map[string]string{
			"registration_state":      `ALTER TABLE projects ADD COLUMN registration_state TEXT NOT NULL DEFAULT 'healthy'`,
			"claim_generation":        `ALTER TABLE projects ADD COLUMN claim_generation INTEGER NOT NULL DEFAULT 1`,
			"attestation_fingerprint": `ALTER TABLE projects ADD COLUMN attestation_fingerprint TEXT NOT NULL DEFAULT ''`,
		} {
			present, err := projectRegistryColumnPresent(db, "projects", name)
			if err != nil {
				return err
			}
			if !present {
				if _, err := db.Exec(statement); err != nil {
					return err
				}
			}
		}
		// Legacy schemas did not have foreign keys. Remove only metadata rows
		// whose project claim no longer exists; never touch source workspaces.
		for _, statement := range []string{
			`DELETE FROM project_profiles WHERE NOT EXISTS (SELECT 1 FROM projects p WHERE p.alias=project_profiles.alias)`,
			`DELETE FROM project_workspaces WHERE NOT EXISTS (SELECT 1 FROM projects p WHERE p.alias=project_workspaces.alias)`,
		} {
			if _, err := db.Exec(statement); err != nil {
				return err
			}
		}
		if _, err := db.Exec(`PRAGMA user_version=2`); err != nil {
			return err
		}
	}
	return nil
}

func projectRegistryColumnPresent(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func NormalizeProjectAlias(raw string) (string, error) {
	alias := strings.ToLower(strings.TrimSpace(raw))
	if !projectAliasPattern.MatchString(alias) {
		return "", projectErr(ProjectErrorInvalidInput, errors.New("project alias is invalid"))
	}
	return alias, nil
}

func normalizeProjectTarget(raw string) (string, error) {
	target := strings.ToLower(strings.TrimSpace(raw))
	if !projectTargetPattern.MatchString(target) {
		return "", projectErr(ProjectErrorInvalidInput, errors.New("project target alias is invalid"))
	}
	return target, nil
}

func NormalizeProjectRepository(rawOwner, rawRepository string) (string, string, error) {
	owner := strings.ToLower(strings.TrimSpace(rawOwner))
	repository := strings.ToLower(strings.TrimSpace(rawRepository))
	if !githubOwnerPattern.MatchString(owner) || !devGitSimplePattern.MatchString(repository) ||
		strings.ContainsAny(repository, `/\\`) || strings.HasPrefix(repository, ".") {
		return "", "", projectErr(ProjectErrorInvalidInput, errors.New("project repository identity is invalid"))
	}
	return owner, repository, nil
}

func CanonicalProjectPath(roots WorkspaceRoots, repository string) (string, error) {
	roots, err := normalizeWorkspaceRoots(roots)
	if err != nil {
		return "", err
	}
	_, repository, err = NormalizeProjectRepository("owner", repository)
	if err != nil {
		return "", err
	}
	root, err := projectDevelopmentRoot(roots)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, repository)
	if filepath.Dir(candidate) != filepath.Clean(root) || !pathInside(root, candidate) {
		return "", projectErr(ProjectErrorInvalidInput, errors.New("canonical project path escaped the development root"))
	}
	return candidate, nil
}

func (r *ProjectRegistry) Register(input ProjectRegistration) (Project, bool, error) {
	if r == nil || r.db == nil || r.workspaces == nil || r.inspector == nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	alias, err := NormalizeProjectAlias(input.Alias)
	if err != nil {
		return Project{}, false, err
	}
	owner, repository, err := NormalizeProjectRepository(input.Owner, input.Repository)
	if err != nil {
		return Project{}, false, err
	}
	if owner != r.allowedOwner {
		return Project{}, false, projectErr(ProjectErrorOwnerDenied, errors.New("project owner is not allowed"))
	}
	preferredTarget, err := normalizeProjectTarget(input.PreferredTarget)
	if err != nil {
		return Project{}, false, err
	}
	targetAlias, err := normalizeProjectTarget(input.TargetAlias)
	if err != nil || targetAlias != preferredTarget || !workspaceIDPattern.MatchString(input.WorkspaceID) {
		return Project{}, false, projectErr(ProjectErrorInvalidInput, errors.New("initial project binding is invalid"))
	}
	profiles, err := normalizeProjectProfiles(input.AllowedProfiles)
	if err != nil {
		return Project{}, false, err
	}
	workspace, err := r.workspaces.Get(input.WorkspaceID)
	if err != nil {
		return Project{}, false, projectErr(ProjectErrorWorkspaceMissing, err)
	}
	if !containsProjectProfile(profiles, workspace.Profile) {
		return Project{}, false, projectErr(ProjectErrorProfileDenied, errors.New("workspace profile is not allowed for the project"))
	}
	attestation, attestationErr := r.attestationFingerprint(workspace)
	if err := r.inspectCheckout(context.Background(), workspace, owner, repository); err != nil {
		return Project{}, false, err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	defer tx.Rollback()
	if existing, found, lookupErr := loadProjectTx(tx, alias); lookupErr != nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, lookupErr)
	} else if found {
		if !existing.ClaimGenerationValid {
			return Project{}, false, projectErr(ProjectErrorPlanChanged, errors.New("project claim generation is invalid"))
		}
		if attestationErr != nil {
			return Project{}, false, projectAttestationFailure(workspace.Path, attestationErr)
		}
		idempotent, compareErr := r.registrationMatchesTx(tx, existing, targetAlias, workspace.ID, profiles, owner, repository, preferredTarget, attestation)
		if compareErr != nil {
			return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, compareErr)
		}
		if !idempotent {
			return Project{}, false, projectErr(ProjectErrorAliasConflict, errors.New("project alias is already bound"))
		}
		// v1 rows have no attestation. A successful revalidation above is the
		// only point at which the durable identity may be adopted lazily.
		if existing.AttestationFingerprint == "" {
			if _, err := tx.Exec(`UPDATE projects SET attestation_fingerprint=?,updated_at=? WHERE alias=? AND attestation_fingerprint=''`, attestation, r.now().UTC().UnixNano(), alias); err != nil {
				return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
			}
			existing.AttestationFingerprint = attestation
		}
		return existing, false, nil
	}
	if attestationErr != nil {
		return Project{}, false, projectAttestationFailure(workspace.Path, attestationErr)
	}
	var existingAlias, registrationState string
	if err := tx.QueryRow(`SELECT alias,registration_state FROM projects WHERE owner=? AND repository=?`, owner, repository).Scan(&existingAlias, &registrationState); err == nil {
		claim := &ProjectClaim{Alias: existingAlias, Owner: owner, Repository: repository, State: ProjectClaimState(registrationState)}
		return Project{}, false, &ProjectError{Code: ProjectErrorRepositoryConflict, Err: errors.New("repository is already registered under another alias"), Claim: claim}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if err := tx.QueryRow(`SELECT alias FROM project_workspaces WHERE workspace_id=?`, workspace.ID).Scan(&existingAlias); err == nil {
		return Project{}, false, projectErr(ProjectErrorWorkspaceConflict, errors.New("workspace is already bound to another project"))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	now := r.now().UTC()
	if _, err := tx.Exec(`INSERT INTO projects(alias,owner,repository,preferred_target,attestation_fingerprint,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		alias, owner, repository, preferredTarget, attestation, now.UnixNano(), now.UnixNano()); err != nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	for _, profile := range profiles {
		if _, err := tx.Exec(`INSERT INTO project_profiles(alias,profile) VALUES(?,?)`, alias, profile); err != nil {
			return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO project_workspaces(alias,target_alias,workspace_id,created_at,updated_at) VALUES(?,?,?,?,?)`,
		alias, targetAlias, workspace.ID, now.UnixNano(), now.UnixNano()); err != nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	return Project{Alias: alias, Owner: owner, Repository: repository, PreferredTarget: preferredTarget, ClaimGeneration: 1, ClaimGenerationValid: true, AttestationFingerprint: attestation, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (r *ProjectRegistry) Resolve(ctx context.Context, rawAlias, rawTarget string) (ProjectResolution, error) {
	if r == nil || r.db == nil || r.workspaces == nil || r.inspector == nil {
		return ProjectResolution{}, projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	resolution, err := r.ResolveRegistered(rawAlias, rawTarget)
	if err != nil {
		return ProjectResolution{}, err
	}
	observation, err := r.inspectCheckoutObservation(ctx, resolution.Workspace, resolution.Project.Owner, resolution.Project.Repository)
	if err != nil {
		return ProjectResolution{}, err
	}
	if observation.State != ProjectCheckoutReady && observation.State != ProjectCheckoutDirty {
		return ProjectResolution{}, projectErrorForCheckoutObservation(observation)
	}
	resolution.CheckoutState = observation.State
	resolution.RegisteredOnly = false
	if observation.Diagnostic.Reason != "" {
		resolution.CheckoutDiagnostic = cloneProjectCheckoutDiagnostic(observation.Diagnostic)
	}
	return resolution, nil
}

// ResolveRegistered loads only the durable project/workspace binding and
// revalidates the registered workspace boundary. It deliberately does not run
// Git metadata or status commands, so process control and other registry-only
// consumers do not fail because a developer is changing the checkout.
func (r *ProjectRegistry) ResolveRegistered(rawAlias, rawTarget string) (ProjectResolution, error) {
	if r == nil || r.db == nil || r.workspaces == nil {
		return ProjectResolution{}, projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	alias, err := NormalizeProjectAlias(rawAlias)
	if err != nil {
		return ProjectResolution{}, err
	}
	project, found, err := loadProject(r.db, alias)
	if err != nil {
		return ProjectResolution{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if !found {
		return ProjectResolution{}, projectErr(ProjectErrorProjectNotFound, errors.New("project alias is not registered"))
	}
	if !project.ClaimGenerationValid {
		return ProjectResolution{}, projectErr(ProjectErrorPlanChanged, errors.New("project claim generation is invalid"))
	}
	target := project.PreferredTarget
	if strings.TrimSpace(rawTarget) != "" {
		target, err = normalizeProjectTarget(rawTarget)
		if err != nil {
			return ProjectResolution{}, err
		}
	}
	var workspaceID string
	if err := r.db.QueryRow(`SELECT workspace_id FROM project_workspaces WHERE alias=? AND target_alias=?`, alias, target).Scan(&workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectResolution{}, projectErr(ProjectErrorTargetNotFound, errors.New("project target is not bound"))
		}
		return ProjectResolution{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	profiles, err := loadProjectProfiles(r.db, alias)
	if err != nil {
		return ProjectResolution{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	workspace, err := r.workspaces.Get(workspaceID)
	if err != nil {
		return ProjectResolution{}, projectErr(ProjectErrorWorkspaceMissing, err)
	}
	if !containsProjectProfile(profiles, workspace.Profile) {
		return ProjectResolution{}, projectErr(ProjectErrorProfileDenied, errors.New("workspace profile is no longer allowed"))
	}
	if project.AttestationFingerprint == "" {
		diagnostic := ProjectCheckoutDiagnostic{Reason: "workspace_attestation_missing", Repairable: true, RecommendedAction: "project_reconcile"}
		return ProjectResolution{}, &ProjectError{Code: ProjectErrorCheckoutMissing, Err: errors.New(diagnostic.Reason), Diagnostic: &diagnostic}
	}
	observed, fingerprintErr := r.attestationFingerprint(workspace)
	if fingerprintErr != nil {
		return ProjectResolution{}, projectErr(ProjectErrorCheckoutUnavailable, fingerprintErr)
	}
	if observed != project.AttestationFingerprint {
		diagnostic := ProjectCheckoutDiagnostic{Reason: "workspace_identity_mismatch", Expected: project.AttestationFingerprint, Observed: observed, Repairable: true, RecommendedAction: "project_reconcile"}
		return ProjectResolution{}, &ProjectError{Code: ProjectErrorCheckoutIdentityMismatch, Err: errors.New(diagnostic.Reason), Diagnostic: &diagnostic}
	}
	return ProjectResolution{Project: project, TargetAlias: target, Workspace: workspace, CheckoutState: ProjectCheckoutRegistered, RegisteredOnly: true}, nil
}

func (r *ProjectRegistry) inspectCheckout(ctx context.Context, workspace Workspace, owner, repository string) error {
	state, err := r.inspectCheckoutState(ctx, workspace, owner, repository)
	if err != nil {
		return err
	}
	if state == ProjectCheckoutDirty {
		diagnostic := ProjectCheckoutDiagnostic{Reason: "normal_workspace_changes", Path: workspace.Path, Repairable: false}
		return &ProjectError{Code: ProjectErrorCheckoutDirty, Err: errors.New("project checkout has local changes"), Diagnostic: &diagnostic}
	}
	return nil
}

func (r *ProjectRegistry) inspectCheckoutState(ctx context.Context, workspace Workspace, owner, repository string) (ProjectCheckoutState, error) {
	observation, err := r.inspectCheckoutObservation(ctx, workspace, owner, repository)
	if err != nil {
		return "", err
	}
	if observation.State == ProjectCheckoutReady || observation.State == ProjectCheckoutDirty {
		return observation.State, nil
	}
	return "", projectErrorForCheckoutObservation(observation)
}

func (r *ProjectRegistry) inspectCheckoutObservation(ctx context.Context, workspace Workspace, owner, repository string) (ProjectCheckoutObservation, error) {
	if detailed, ok := r.inspector.(ProjectCheckoutInspectorV2); ok {
		observation, err := detailed.InspectDetailed(ctx, workspace.Path, owner, repository)
		if err != nil {
			return checkoutObservationFromError(ctx, workspace.Path, err), nil
		}
		return normalizeProjectCheckoutObservation(observation, workspace.Path), nil
	}
	state, err := r.inspector.Inspect(ctx, workspace.Path, owner, repository)
	if err != nil {
		return classifyLegacyCheckoutError(ctx, workspace.Path, err), nil
	}
	return normalizeProjectCheckoutObservation(ProjectCheckoutObservation{State: state}, workspace.Path), nil
}

func checkoutObservationFromError(ctx context.Context, path string, err error) ProjectCheckoutObservation {
	state := ProjectCheckoutUnavailable
	reason := "checkout_inspection_failed"
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		state = ProjectCheckoutTimeout
		reason = "checkout_inspection_timeout"
	}
	return ProjectCheckoutObservation{State: state, Diagnostic: ProjectCheckoutDiagnostic{
		Reason: reason, Path: path, Observed: sanitizeCheckoutValue(err.Error()), Repairable: true, RecommendedAction: "project_reconcile",
	}}
}

func classifyLegacyCheckoutError(ctx context.Context, path string, err error) ProjectCheckoutObservation {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unsafe") || strings.Contains(message, "symlink") || strings.Contains(message, "owned") || strings.Contains(message, "escaped") {
		return ProjectCheckoutObservation{State: ProjectCheckoutUnsafeBoundary, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "workspace_boundary_violation", Path: path, Observed: sanitizeCheckoutValue(err.Error()), Repairable: false,
		}}
	}
	if strings.Contains(message, "metadata") || strings.Contains(message, "repository root") || strings.Contains(message, "checkout root") {
		return ProjectCheckoutObservation{State: ProjectCheckoutCorrupt, Diagnostic: ProjectCheckoutDiagnostic{
			Reason: "checkout_corrupt", Path: path, Observed: sanitizeCheckoutValue(err.Error()), Repairable: true, RecommendedAction: "project_reconcile",
		}}
	}
	return checkoutObservationFromError(ctx, path, err)
}

func projectAttestationFailure(path string, err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "unsafe") || strings.Contains(message, "symlink") || strings.Contains(message, "escaped") || strings.Contains(message, "replaced") {
		diagnostic := ProjectCheckoutDiagnostic{Reason: "workspace_boundary_violation", Path: path, Repairable: false}
		return &ProjectError{Code: ProjectErrorCheckoutUnsafeBoundary, Err: errors.New(diagnostic.Reason), Diagnostic: &diagnostic}
	}
	diagnostic := ProjectCheckoutDiagnostic{Reason: "workspace_attestation_unavailable", Path: path, Repairable: true, RecommendedAction: "project_reconcile"}
	return &ProjectError{Code: ProjectErrorCheckoutUnavailable, Err: errors.New(diagnostic.Reason), Diagnostic: &diagnostic}
}

func normalizeProjectCheckoutObservation(observation ProjectCheckoutObservation, path string) ProjectCheckoutObservation {
	if observation.Diagnostic.Path == "" {
		observation.Diagnostic.Path = path
	}
	if observation.Diagnostic.Reason == "" {
		switch observation.State {
		case ProjectCheckoutReady:
			observation.Diagnostic.Reason = "checkout_ready"
		case ProjectCheckoutDirty:
			observation.Diagnostic.Reason = "normal_workspace_changes"
		case ProjectCheckoutRemoteMismatch:
			observation.Diagnostic.Reason = "repository_identity_mismatch"
		case ProjectCheckoutUnsafe:
			observation.Diagnostic.Reason = "legacy_unsafe_checkout"
		case ProjectCheckoutUnsafeBoundary:
			observation.Diagnostic.Reason = "workspace_boundary_violation"
		case ProjectCheckoutCorrupt:
			observation.Diagnostic.Reason = "checkout_corrupt"
		case ProjectCheckoutTimeout:
			observation.Diagnostic.Reason = "checkout_inspection_timeout"
		default:
			observation.Diagnostic.Reason = "checkout_unavailable"
		}
	}
	return observation
}

func projectErrorForCheckoutObservation(observation ProjectCheckoutObservation) error {
	observation = normalizeProjectCheckoutObservation(observation, observation.Diagnostic.Path)
	diagnostic := cloneProjectCheckoutDiagnostic(observation.Diagnostic)
	var code ProjectErrorCode
	switch observation.State {
	case ProjectCheckoutRemoteMismatch:
		code = ProjectErrorRepositoryMismatch
	case ProjectCheckoutIdentityMismatch:
		code = ProjectErrorCheckoutIdentityMismatch
	case ProjectCheckoutCorrupt:
		code = ProjectErrorCheckoutCorrupt
	case ProjectCheckoutUnsafeBoundary:
		code = ProjectErrorCheckoutUnsafeBoundary
	case ProjectCheckoutTimeout:
		code = ProjectErrorCheckoutTimeout
	case ProjectCheckoutUnavailable:
		code = ProjectErrorCheckoutUnavailable
	case ProjectCheckoutUnsafe:
		code = ProjectErrorCheckoutUnsafe
	default:
		code = ProjectErrorCheckoutUnavailable
	}
	return &ProjectError{Code: code, Err: errors.New(diagnostic.Reason), Diagnostic: diagnostic}
}

func projectErrorCodeForCheckoutObservation(observation ProjectCheckoutObservation) ProjectErrorCode {
	switch observation.State {
	case ProjectCheckoutRemoteMismatch:
		return ProjectErrorRepositoryMismatch
	case ProjectCheckoutIdentityMismatch:
		return ProjectErrorCheckoutIdentityMismatch
	case ProjectCheckoutCorrupt:
		return ProjectErrorCheckoutCorrupt
	case ProjectCheckoutUnsafeBoundary:
		return ProjectErrorCheckoutUnsafeBoundary
	case ProjectCheckoutTimeout:
		return ProjectErrorCheckoutTimeout
	case ProjectCheckoutUnsafe:
		return ProjectErrorCheckoutUnsafe
	default:
		return ProjectErrorCheckoutUnavailable
	}
}

func cloneProjectCheckoutDiagnostic(diagnostic ProjectCheckoutDiagnostic) *ProjectCheckoutDiagnostic {
	copy := diagnostic
	return &copy
}

func (r *ProjectRegistry) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *ProjectRegistry) attestationFingerprint(workspace Workspace) (string, error) {
	if workspace.Profile == WorkspaceProfileSandbox {
		return ProjectAttestationFingerprint(workspace.Path)
	}
	if r == nil || r.workspaces == nil {
		return "", errors.New("project registry workspace roots are unavailable")
	}
	roots := r.workspaces.roots
	return projectAttestationFingerprintWithRoots(workspace.Path, &roots)
}

// ListClaims reports all durable repository claims. It performs a bounded
// repository-identity observation when a claim has an attestation, but never
// runs Git status/discovery or mutates a workspace. A missing workspace row or
// failed workspace boundary revalidation is reported as stale/repairable
// instead of being silently treated as an unknown alias.
func (r *ProjectRegistry) ListClaims() ([]ProjectClaim, error) {
	return r.ListClaimsContext(context.Background(), "")
}

// ListClaimsContext is the context-aware, optionally target-filtered form of
// ListClaims. The target is normalized before it reaches SQLite so callers
// cannot use a registry listing to observe another target by accident.
func (r *ProjectRegistry) ListClaimsContext(ctx context.Context, target string) ([]ProjectClaim, error) {
	if r == nil || r.db == nil || r.workspaces == nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	target = strings.TrimSpace(target)
	if target != "" {
		var err error
		target, err = normalizeProjectTarget(target)
		if err != nil {
			return nil, err
		}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.alias,p.owner,p.repository,p.preferred_target,
		       COALESCE(p.registration_state,'healthy'),
		       COALESCE(wb.target_alias,''),COALESCE(wb.workspace_id,''),
		       COALESCE(p.claim_generation,1),COALESCE(p.attestation_fingerprint,'')
		FROM projects p
		LEFT JOIN project_workspaces wb ON wb.alias=p.alias
		WHERE p.owner=? AND (?='' OR wb.target_alias=? OR (wb.target_alias IS NULL AND p.preferred_target=?))
		ORDER BY p.alias,wb.target_alias LIMIT 33`, r.allowedOwner, target, target, target)
	if err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	// Materialize and close the SQLite cursor before any filesystem or Git
	// observation. The registry uses a single connection; inspecting while
	// rows are open can otherwise block unrelated registry operations.
	type claimRow struct {
		claim           ProjectClaim
		storedState     string
		boundTarget     string
		attestation     string
		generationValid bool
	}
	rowsData := make([]claimRow, 0)
	for rows.Next() {
		var row claimRow
		var generation int64
		if err := rows.Scan(&row.claim.Alias, &row.claim.Owner, &row.claim.Repository, &row.claim.Target, &row.storedState, &row.boundTarget, &row.claim.WorkspaceID, &generation, &row.attestation); err != nil {
			_ = rows.Close()
			return nil, projectErr(ProjectErrorRegistryUnavailable, err)
		}
		row.generationValid = generation > 0 && uint64(generation) <= maxProjectClaimGeneration
		if row.generationValid {
			row.claim.Generation = uint64(generation)
		} else {
			row.claim.Generation = 1
		}
		rowsData = append(rowsData, row)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, rowsErr)
	}
	if closeErr != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, closeErr)
	}
	claims := make([]ProjectClaim, 0, len(rowsData))
	for _, row := range rowsData {
		claim := row.claim
		storedState := row.storedState
		boundTarget := row.boundTarget
		attestation := row.attestation
		generationValid := row.generationValid
		if boundTarget != "" {
			claim.Target = boundTarget
		}
		claim.State = ProjectClaimState(storedState)
		if claim.State != ProjectClaimHealthy && claim.State != ProjectClaimStale && claim.State != ProjectClaimRepairable {
			claim.State = ProjectClaimStale
		}
		if !generationValid {
			// Do not let a matching filesystem attestation overwrite a
			// malformed durable generation and make it look healthy.
			claim.State = ProjectClaimRepairable
			claim.Reason = ProjectErrorPlanChanged
			claim.Repairable = true
		} else if claim.WorkspaceID == "" {
			claim.State = ProjectClaimRepairable
			claim.Reason = ProjectErrorTargetNotFound
			claim.Repairable = true
		} else if _, workspaceErr := r.workspaces.Get(claim.WorkspaceID); workspaceErr != nil {
			claim.State = ProjectClaimStale
			claim.Reason = ProjectErrorWorkspaceMissing
			claim.Repairable = true
		} else if attestation != "" {
			workspace, workspaceErr := r.workspaces.LookupRegistered(claim.WorkspaceID)
			if workspaceErr != nil {
				claim.State = ProjectClaimStale
				claim.Reason = ProjectErrorWorkspaceMissing
				claim.Repairable = true
			} else if observed, fingerprintErr := r.attestationFingerprint(workspace); fingerprintErr != nil {
				claim.State = ProjectClaimStale
				claim.Reason = ProjectErrorCheckoutUnavailable
				claim.Repairable = true
			} else if observed != attestation {
				claim.State = ProjectClaimStale
				claim.Reason = ProjectErrorCheckoutIdentityMismatch
				claim.Repairable = true
			} else {
				// The filesystem attestation intentionally excludes mutable Git
				// config. Run only the bounded repository identity check here so
				// changing origin cannot make the registry claim appear healthy
				// while project_status reports a repository mismatch.
				claim.State = ProjectClaimHealthy
				claim.Reason = ""
				claim.Repairable = false
				if identityInspector, ok := r.inspector.(ProjectRepositoryIdentityInspector); ok {
					observation, inspectErr := identityInspector.InspectRepositoryIdentity(ctx, workspace.Path, claim.Owner, claim.Repository)
					if inspectErr != nil {
						claim.State = ProjectClaimStale
						claim.Reason = ProjectErrorCheckoutUnavailable
						claim.Repairable = true
					} else {
						observation = normalizeProjectCheckoutObservation(observation, workspace.Path)
						switch observation.State {
						case ProjectCheckoutIdentityMismatch, ProjectCheckoutRemoteMismatch:
							claim.State = ProjectClaimStale
							claim.Reason = ProjectErrorRepositoryMismatch
							claim.Repairable = true
						case ProjectCheckoutReady, ProjectCheckoutDirty:
							// Dirty source is an ordinary development state; the
							// owner/repository identity still matches.
						default:
							claim.State = ProjectClaimStale
							claim.Reason = projectErrorCodeForCheckoutObservation(observation)
							claim.Repairable = true
						}
					}
				}
			}
		} else {
			// Legacy rows have no durable identity attestation. Do not report
			// those claims as healthy before a boundary and Git identity check
			// has adopted one through ReconcileClaim.
			claim.State = ProjectClaimRepairable
			claim.Reason = ProjectErrorCheckoutMissing
			claim.Repairable = true
		}
		claims = append(claims, claim)
	}
	if len(claims) > maxProjectClaims {
		return nil, projectErr(ProjectErrorDiscoveryLimit, errors.New("project registry claim limit exceeded"))
	}
	return claims, nil
}

// ReconcileClaims persists only the derived lifecycle state of registry
// claims. It never removes a repository, workspace or project row.
func (r *ProjectRegistry) ReconcileClaims() ([]ProjectClaim, error) {
	claims, err := r.ListClaims()
	if err != nil {
		return nil, err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	defer tx.Rollback()
	now := r.now().UTC().UnixNano()
	for _, claim := range claims {
		result, err := tx.Exec(`UPDATE projects SET registration_state=?,updated_at=? WHERE alias=? AND owner=? AND repository=? AND claim_generation=? AND (?='' OR EXISTS (SELECT 1 FROM project_workspaces WHERE alias=? AND target_alias=? AND workspace_id=?))`,
			claim.State, now, claim.Alias, claim.Owner, claim.Repository, claim.Generation, claim.WorkspaceID, claim.Alias, claim.Target, claim.WorkspaceID)
		if err != nil {
			return nil, projectErr(ProjectErrorRegistryUnavailable, err)
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return nil, projectErr(ProjectErrorPlanChanged, errors.New("project claim changed during reconciliation"))
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	return claims, nil
}

// ReconcileClaim applies the derived lifecycle state to one exact alias and
// target. Keeping the write scoped prevents a recovery request for one
// project from rewriting unrelated registry rows.
func (r *ProjectRegistry) ReconcileClaim(ctx context.Context, alias, target string) (ProjectClaim, error) {
	alias, err := NormalizeProjectAlias(alias)
	if err != nil {
		return ProjectClaim{}, err
	}
	target, err = normalizeProjectTarget(target)
	if err != nil {
		return ProjectClaim{}, err
	}
	project, found, err := loadProject(r.db, alias)
	if err != nil {
		return ProjectClaim{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if !found {
		return ProjectClaim{}, projectErr(ProjectErrorProjectNotFound, errors.New("project claim is not registered"))
	}
	if project.Owner != r.allowedOwner {
		return ProjectClaim{}, projectErr(ProjectErrorOwnerDenied, errors.New("project claim owner is not allowed"))
	}
	if !project.ClaimGenerationValid {
		return ProjectClaim{}, projectErr(ProjectErrorPlanChanged, errors.New("project claim generation is invalid"))
	}
	var workspaceID string
	if err := r.db.QueryRow(`SELECT workspace_id FROM project_workspaces WHERE alias=? AND target_alias=?`, alias, target).Scan(&workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectClaim{}, projectErr(ProjectErrorTargetNotFound, errors.New("project claim target is not registered"))
		}
		return ProjectClaim{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	workspace, err := r.workspaces.Get(workspaceID)
	if err != nil {
		return ProjectClaim{}, projectErr(ProjectErrorWorkspaceMissing, err)
	}
	profiles, err := loadProjectProfiles(r.db, alias)
	if err != nil {
		return ProjectClaim{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if !containsProjectProfile(profiles, workspace.Profile) {
		return ProjectClaim{}, projectErr(ProjectErrorProfileDenied, errors.New("workspace profile is no longer allowed"))
	}
	// Capture the boundary identity before the remote/Git observation. The
	// observation itself may take time, and a same-user process can otherwise
	// replace the registered checkout between validation and persistence.
	beforeAttestation, err := r.attestationFingerprint(workspace)
	if err != nil {
		return ProjectClaim{}, projectAttestationFailure(workspace.Path, err)
	}
	// Reconciliation is a real identity check, not a metadata-only repair:
	// validate the registered boundary and the Git owner/repository before
	// adopting a changed filesystem attestation.
	observation, err := r.inspectCheckoutObservation(ctx, workspace, project.Owner, project.Repository)
	if err != nil {
		return ProjectClaim{}, err
	}
	if observation.State != ProjectCheckoutReady && observation.State != ProjectCheckoutDirty {
		return ProjectClaim{}, projectErrorForCheckoutObservation(observation)
	}
	afterAttestation, err := r.attestationFingerprint(workspace)
	if err != nil {
		return ProjectClaim{}, projectAttestationFailure(workspace.Path, err)
	}
	if beforeAttestation != afterAttestation {
		diagnostic := ProjectCheckoutDiagnostic{
			Reason: "workspace_changed_during_reconciliation", Path: workspace.Path,
			Expected: beforeAttestation, Observed: afterAttestation,
			Repairable: true, RecommendedAction: "project_reconcile",
		}
		return ProjectClaim{}, &ProjectError{Code: ProjectErrorPlanChanged, Err: errors.New(diagnostic.Reason), Diagnostic: &diagnostic}
	}
	now := r.now().UTC().UnixNano()
	result, err := r.db.Exec(`UPDATE projects SET registration_state=?,attestation_fingerprint=?,updated_at=? WHERE alias=? AND owner=? AND repository=? AND claim_generation=? AND EXISTS (SELECT 1 FROM project_workspaces WHERE alias=? AND target_alias=? AND workspace_id=?)`, string(ProjectClaimHealthy), afterAttestation, now, alias, project.Owner, project.Repository, project.ClaimGeneration, alias, target, workspace.ID)
	if err != nil {
		return ProjectClaim{}, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ProjectClaim{}, projectErr(ProjectErrorPlanChanged, errors.New("project claim changed during reconciliation"))
	}
	return ProjectClaim{Alias: project.Alias, Owner: project.Owner, Repository: project.Repository, Target: target, WorkspaceID: workspace.ID, Generation: project.ClaimGeneration, State: ProjectClaimHealthy}, nil
}

// ReleaseClaim is intentionally limited to stale claims and requires the
// caller to restate the normalized owner/repository and the exact generation
// observed by the caller. It releases registry metadata only; the source
// workspace and its contents are never removed.
func (r *ProjectRegistry) ReleaseClaim(alias, owner, repository, target string, generation uint64) error {
	if r == nil || r.db == nil || r.workspaces == nil {
		return projectErr(ProjectErrorRegistryUnavailable, errors.New("project registry is unavailable"))
	}
	if generation == 0 || generation > maxProjectClaimGeneration {
		return projectErr(ProjectErrorInvalidInput, errors.New("project claim generation is invalid"))
	}
	alias, err := NormalizeProjectAlias(alias)
	if err != nil {
		return err
	}
	owner, repository, err = NormalizeProjectRepository(owner, repository)
	if err != nil {
		return err
	}
	target, err = normalizeProjectTarget(target)
	if err != nil {
		return err
	}
	if owner != r.allowedOwner {
		return projectErr(ProjectErrorOwnerDenied, errors.New("project claim owner is not allowed"))
	}
	claims, err := r.ListClaims()
	if err != nil {
		return err
	}
	var selected *ProjectClaim
	for index := range claims {
		claim := claims[index]
		if claim.Alias == alias && claim.Target == target {
			selected = &claim
			break
		}
	}
	if selected == nil {
		return projectErr(ProjectErrorProjectNotFound, errors.New("project alias is not registered"))
	}
	if selected.Owner != owner || selected.Repository != repository {
		return projectErr(ProjectErrorAliasConflict, errors.New("project claim identity does not match"))
	}
	if selected.State == ProjectClaimHealthy {
		return projectErr(ProjectErrorClaimHealthy, errors.New("healthy project claims require explicit reassociation"))
	}
	tx, err := r.db.Begin()
	if err != nil {
		return projectErr(ProjectErrorRegistryUnavailable, err)
	}
	defer tx.Rollback()
	var storedGeneration int64
	var storedWorkspaceID, storedAttestation, preferredTarget string
	if err := tx.QueryRow(`
		SELECT p.claim_generation,p.attestation_fingerprint,p.preferred_target,COALESCE(wb.workspace_id,'')
		FROM projects p LEFT JOIN project_workspaces wb ON wb.alias=p.alias AND wb.target_alias=?
		WHERE p.alias=? AND p.owner=? AND p.repository=?
		  AND (wb.workspace_id IS NOT NULL OR (p.preferred_target=? AND NOT EXISTS (SELECT 1 FROM project_workspaces WHERE alias=p.alias)))`, target, alias, owner, repository, target).Scan(&storedGeneration, &storedAttestation, &preferredTarget, &storedWorkspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return projectErr(ProjectErrorTargetNotFound, errors.New("project claim target is not registered"))
		}
		return projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if storedGeneration <= 0 || uint64(storedGeneration) != generation {
		return projectErr(ProjectErrorPlanChanged, errors.New("project claim generation changed"))
	}
	// Acquire the project row and confirm the attestation used by the caller
	// is still authoritative before removing the target binding. This closes
	// the race where a concurrent reconciliation makes a stale claim healthy
	// after ListClaims but before the delete.
	guard, guardErr := tx.Exec(`UPDATE projects SET updated_at=updated_at WHERE alias=? AND owner=? AND repository=? AND claim_generation=? AND attestation_fingerprint=?`, alias, owner, repository, generation, storedAttestation)
	if guardErr != nil {
		return projectErr(ProjectErrorRegistryUnavailable, guardErr)
	}
	if count, _ := guard.RowsAffected(); count != 1 {
		return projectErr(ProjectErrorPlanChanged, errors.New("project claim changed during release"))
	}
	// Re-check the authoritative filesystem and repository identity after the
	// exact row and generation are locked. A concurrent reconciliation may
	// have made the cached state healthy since ListClaims; never delete such a
	// claim. The Git identity check is required here because remote mutation is
	// deliberately excluded from the physical workspace attestation.
	if storedWorkspaceID != "" && storedAttestation != "" {
		workspace, workspaceErr := r.workspaces.Get(storedWorkspaceID)
		if workspaceErr == nil {
			observed, fingerprintErr := r.attestationFingerprint(workspace)
			if fingerprintErr == nil && observed == storedAttestation {
				observation, inspectErr := r.inspectCheckoutObservation(context.Background(), workspace, owner, repository)
				if inspectErr == nil && (observation.State == ProjectCheckoutReady || observation.State == ProjectCheckoutDirty) {
					return projectErr(ProjectErrorClaimHealthy, errors.New("healthy project claims require explicit reassociation"))
				}
			}
		}
	}
	if storedWorkspaceID != "" {
		result, deleteErr := tx.Exec(`DELETE FROM project_workspaces WHERE alias=? AND target_alias=? AND workspace_id=?`, alias, target, storedWorkspaceID)
		if deleteErr != nil {
			return projectErr(ProjectErrorRegistryUnavailable, deleteErr)
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return projectErr(ProjectErrorPlanChanged, errors.New("project claim changed during release"))
		}
	}
	var remaining int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM project_workspaces WHERE alias=?`, alias).Scan(&remaining); err != nil {
		return projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if remaining == 0 {
		if _, err := tx.Exec(`DELETE FROM project_profiles WHERE alias=?`, alias); err != nil {
			return projectErr(ProjectErrorRegistryUnavailable, err)
		}
		result, deleteErr := tx.Exec(`DELETE FROM projects WHERE alias=? AND owner=? AND repository=? AND claim_generation=?`, alias, owner, repository, generation)
		if deleteErr != nil {
			return projectErr(ProjectErrorRegistryUnavailable, deleteErr)
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return projectErr(ProjectErrorPlanChanged, errors.New("project claim changed during release"))
		}
	} else if preferredTarget == target {
		var replacement string
		if err := tx.QueryRow(`SELECT target_alias FROM project_workspaces WHERE alias=? ORDER BY target_alias LIMIT 1`, alias).Scan(&replacement); err != nil {
			return projectErr(ProjectErrorRegistryUnavailable, err)
		}
		if _, err := tx.Exec(`UPDATE projects SET preferred_target=?,updated_at=? WHERE alias=? AND owner=? AND repository=? AND claim_generation=?`, replacement, r.now().UTC().UnixNano(), alias, owner, repository, generation); err != nil {
			return projectErr(ProjectErrorRegistryUnavailable, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return projectErr(ProjectErrorRegistryUnavailable, err)
	}
	return nil
}

func (resolution ProjectResolution) SafeStatus() ProjectStatus {
	return ProjectStatus{
		Alias:      resolution.Project.Alias,
		Repository: resolution.Project.Owner + "/" + resolution.Project.Repository,
		Target:     resolution.TargetAlias,
		State:      resolution.SafeState(),
		Profile:    resolution.Workspace.Profile,
		Mode:       resolution.Workspace.Mode,
		Diagnostic: cloneProjectCheckoutDiagnosticValue(resolution.CheckoutDiagnostic),
	}
}

func cloneProjectCheckoutDiagnosticValue(diagnostic *ProjectCheckoutDiagnostic) *ProjectCheckoutDiagnostic {
	if diagnostic == nil {
		return nil
	}
	copy := *diagnostic
	// Workspace paths are private Edge state. The public diagnostic identifies
	// the failed invariant without returning either an absolute path or a
	// basename that can disclose a local project layout.
	copy.Path = ""
	copy.Expected = publicCheckoutValue(copy.Expected)
	// Git remote identities are bounded and owner-scoped. Arbitrary command
	// errors may contain absolute paths or secrets, so they are not exposed by
	// the public status view.
	copy.Observed = publicCheckoutObservedValue(copy.Observed)
	return &copy
}

func publicCheckoutObservedValue(value string) string {
	value = sanitizeCheckoutValue(value)
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || !strings.HasSuffix(strings.ToLower(parts[1]), ".git") {
		return ""
	}
	owner, repository, err := NormalizeProjectRepository(parts[0], parts[1][:len(parts[1])-4])
	if err != nil {
		return ""
	}
	return "https://github.com/" + owner + "/" + repository + ".git"
}

func publicCheckoutValue(value string) string {
	value = sanitizeCheckoutValue(value)
	if filepath.IsAbs(value) {
		value = filepath.Base(filepath.Clean(value))
	}
	if value == "." || value == ".." || strings.HasPrefix(value, ".."+string(filepath.Separator)) {
		return ""
	}
	return value
}

func (resolution ProjectResolution) SafeState() string {
	if resolution.CheckoutState == ProjectCheckoutRegistered || resolution.RegisteredOnly {
		return string(ProjectCheckoutRegistered)
	}
	if resolution.CheckoutState == ProjectCheckoutDirty {
		return string(ProjectCheckoutDirty)
	}
	return string(ProjectCheckoutReady)
}

func normalizeProjectProfiles(input []WorkspaceProfile) ([]WorkspaceProfile, error) {
	if len(input) == 0 || len(input) > maxProjectProfiles {
		return nil, projectErr(ProjectErrorInvalidInput, errors.New("project profiles are invalid"))
	}
	seen := map[WorkspaceProfile]bool{}
	profiles := make([]WorkspaceProfile, 0, len(input))
	for _, profile := range input {
		if profile != WorkspaceProfileSandbox && profile != WorkspaceProfileLinuxWorkcell && profile != WorkspaceProfileWindowsWorkcell || seen[profile] {
			return nil, projectErr(ProjectErrorInvalidInput, errors.New("project profiles are invalid"))
		}
		seen[profile] = true
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i] < profiles[j] })
	return profiles, nil
}

func containsProjectProfile(profiles []WorkspaceProfile, wanted WorkspaceProfile) bool {
	for _, profile := range profiles {
		if profile == wanted {
			return true
		}
	}
	return false
}

func loadProject(scanner interface{ QueryRow(string, ...any) *sql.Row }, alias string) (Project, bool, error) {
	return scanProject(scanner.QueryRow(`SELECT alias,owner,repository,preferred_target,claim_generation,attestation_fingerprint,created_at,updated_at FROM projects WHERE alias=?`, alias))
}

func loadProjectTx(tx *sql.Tx, alias string) (Project, bool, error) {
	return scanProject(tx.QueryRow(`SELECT alias,owner,repository,preferred_target,claim_generation,attestation_fingerprint,created_at,updated_at FROM projects WHERE alias=?`, alias))
}

func scanProject(row *sql.Row) (Project, bool, error) {
	var project Project
	var createdAt, updatedAt, claimGeneration int64
	if err := row.Scan(&project.Alias, &project.Owner, &project.Repository, &project.PreferredTarget, &claimGeneration, &project.AttestationFingerprint, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, false, nil
		}
		return Project{}, false, err
	}
	valid := claimGeneration > 0 && uint64(claimGeneration) <= maxProjectClaimGeneration
	if !valid {
		claimGeneration = 1
	}
	project.ClaimGeneration = uint64(claimGeneration)
	project.ClaimGenerationValid = valid
	project.CreatedAt = time.Unix(0, createdAt).UTC()
	project.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return project, true, nil
}

func loadProjectProfiles(query interface {
	Query(string, ...any) (*sql.Rows, error)
}, alias string) ([]WorkspaceProfile, error) {
	rows, err := query.Query(`SELECT profile FROM project_profiles WHERE alias=? ORDER BY profile`, alias)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]WorkspaceProfile, 0, maxProjectProfiles)
	for rows.Next() {
		var profile WorkspaceProfile
		if err := rows.Scan(&profile); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return normalizeProjectProfiles(profiles)
}

func (r *ProjectRegistry) registrationMatchesTx(tx *sql.Tx, project Project, targetAlias, workspaceID string, profiles []WorkspaceProfile, owner, repository, preferredTarget, attestation string) (bool, error) {
	if project.Owner != owner || project.Repository != repository || project.PreferredTarget != preferredTarget {
		return false, nil
	}
	if project.AttestationFingerprint != "" && attestation != "" && project.AttestationFingerprint != attestation {
		return false, nil
	}
	var existingWorkspace string
	if err := tx.QueryRow(`SELECT workspace_id FROM project_workspaces WHERE alias=? AND target_alias=?`, project.Alias, targetAlias).Scan(&existingWorkspace); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if existingWorkspace != workspaceID {
		return false, nil
	}
	existingProfiles, err := loadProjectProfiles(tx, project.Alias)
	if err != nil {
		return false, err
	}
	if len(existingProfiles) != len(profiles) {
		return false, nil
	}
	for index := range profiles {
		if existingProfiles[index] != profiles[index] {
			return false, nil
		}
	}
	return true, nil
}

func projectErr(code ProjectErrorCode, err error) error {
	if err == nil {
		err = fmt.Errorf("%s", code)
	}
	return &ProjectError{Code: code, Err: err}
}

func projectErrorIs(err error, code ProjectErrorCode) bool {
	var projectFailure *ProjectError
	return errors.As(err, &projectFailure) && projectFailure.Code == code
}
