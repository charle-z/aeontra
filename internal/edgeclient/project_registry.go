package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	projectRegistryFile  = "projects.db"
	projectSchemaVersion = 1
	maxProjectProfiles   = 4
)

var (
	projectAliasPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	projectTargetPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
)

type ProjectCheckoutState string

const (
	ProjectCheckoutReady          ProjectCheckoutState = "ready"
	ProjectCheckoutDirty          ProjectCheckoutState = "dirty"
	ProjectCheckoutRemoteMismatch ProjectCheckoutState = "remote_mismatch"
	ProjectCheckoutUnsafe         ProjectCheckoutState = "unsafe"
)

type ProjectErrorCode string

const (
	ProjectErrorInvalidInput          ProjectErrorCode = "invalid_input"
	ProjectErrorOwnerDenied           ProjectErrorCode = "owner_denied"
	ProjectErrorAliasConflict         ProjectErrorCode = "alias_conflict"
	ProjectErrorRepositoryConflict    ProjectErrorCode = "repository_conflict"
	ProjectErrorProfileDenied         ProjectErrorCode = "profile_denied"
	ProjectErrorProjectNotFound       ProjectErrorCode = "project_not_found"
	ProjectErrorTargetNotFound        ProjectErrorCode = "target_not_found"
	ProjectErrorWorkspaceMissing      ProjectErrorCode = "workspace_unavailable"
	ProjectErrorWorkspaceConflict     ProjectErrorCode = "workspace_conflict"
	ProjectErrorCheckoutDirty         ProjectErrorCode = "checkout_dirty"
	ProjectErrorRepositoryMismatch    ProjectErrorCode = "repository_mismatch"
	ProjectErrorCheckoutUnsafe        ProjectErrorCode = "checkout_unsafe"
	ProjectErrorCheckoutMissing       ProjectErrorCode = "checkout_missing"
	ProjectErrorAmbiguousCheckout     ProjectErrorCode = "ambiguous_checkout"
	ProjectErrorDiscoveryLimit        ProjectErrorCode = "discovery_limit"
	ProjectErrorDiscoveryTimeout      ProjectErrorCode = "discovery_timeout"
	ProjectErrorPlanChanged           ProjectErrorCode = "plan_changed"
	ProjectErrorPlanExpired           ProjectErrorCode = "plan_expired"
	ProjectErrorRegistryUnavailable   ProjectErrorCode = "registry_unavailable"
	ProjectErrorCredentialUnavailable ProjectErrorCode = "credential_unavailable"
	ProjectErrorCloneFailed           ProjectErrorCode = "clone_failed"
	ProjectErrorCleanupRequired       ProjectErrorCode = "cleanup_required"
)

type ProjectError struct {
	Code ProjectErrorCode
	Err  error
}

func (e *ProjectError) Error() string {
	return "project resolution failed: " + string(e.Code)
}

func (e *ProjectError) Unwrap() error { return e.Err }

type Project struct {
	Alias           string    `json:"alias"`
	Owner           string    `json:"owner"`
	Repository      string    `json:"repository"`
	PreferredTarget string    `json:"preferred_target"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
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

type ProjectRegistry struct {
	db           *sql.DB
	now          func() time.Time
	allowedOwner string
	workspaces   *WorkspaceRegistry
	inspector    ProjectCheckoutInspector
}

type ProjectResolution struct {
	Project     Project
	TargetAlias string
	Workspace   Workspace
}

type ProjectStatus struct {
	Alias      string           `json:"alias"`
	Repository string           `json:"repository"`
	Target     string           `json:"target"`
	State      string           `json:"state"`
	Profile    WorkspaceProfile `json:"profile,omitempty"`
	Mode       WorkspaceMode    `json:"mode,omitempty"`
	Reason     ProjectErrorCode `json:"reason,omitempty"`
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
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	inspector := config.Inspector
	if inspector == nil {
		inspector = newProjectCheckoutInspector()
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
	if version == 0 {
		if _, err := db.Exec(`PRAGMA user_version=1`); err != nil {
			return err
		}
	}
	return nil
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
	candidate := filepath.Join(roots.Dev, repository)
	if filepath.Dir(candidate) != filepath.Clean(roots.Dev) || !pathInside(roots.Dev, candidate) {
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
		idempotent, compareErr := r.registrationMatchesTx(tx, existing, targetAlias, workspace.ID, profiles, owner, repository, preferredTarget)
		if compareErr != nil {
			return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, compareErr)
		}
		if !idempotent {
			return Project{}, false, projectErr(ProjectErrorAliasConflict, errors.New("project alias is already bound"))
		}
		return existing, false, nil
	}
	var existingAlias string
	if err := tx.QueryRow(`SELECT alias FROM projects WHERE owner=? AND repository=?`, owner, repository).Scan(&existingAlias); err == nil {
		return Project{}, false, projectErr(ProjectErrorRepositoryConflict, errors.New("repository is already registered under another alias"))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	if err := tx.QueryRow(`SELECT alias FROM project_workspaces WHERE workspace_id=?`, workspace.ID).Scan(&existingAlias); err == nil {
		return Project{}, false, projectErr(ProjectErrorWorkspaceConflict, errors.New("workspace is already bound to another project"))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, projectErr(ProjectErrorRegistryUnavailable, err)
	}
	now := r.now().UTC()
	if _, err := tx.Exec(`INSERT INTO projects(alias,owner,repository,preferred_target,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		alias, owner, repository, preferredTarget, now.UnixNano(), now.UnixNano()); err != nil {
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
	return Project{Alias: alias, Owner: owner, Repository: repository, PreferredTarget: preferredTarget, CreatedAt: now, UpdatedAt: now}, true, nil
}

func (r *ProjectRegistry) Resolve(ctx context.Context, rawAlias, rawTarget string) (ProjectResolution, error) {
	if r == nil || r.db == nil || r.workspaces == nil || r.inspector == nil {
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
	if err := r.inspectCheckout(ctx, workspace, project.Owner, project.Repository); err != nil {
		return ProjectResolution{}, err
	}
	return ProjectResolution{Project: project, TargetAlias: target, Workspace: workspace}, nil
}

func (r *ProjectRegistry) inspectCheckout(ctx context.Context, workspace Workspace, owner, repository string) error {
	state, err := r.inspector.Inspect(ctx, workspace.Path, owner, repository)
	if err != nil {
		return projectErr(ProjectErrorCheckoutUnsafe, err)
	}
	switch state {
	case ProjectCheckoutReady:
		return nil
	case ProjectCheckoutDirty:
		return projectErr(ProjectErrorCheckoutDirty, errors.New("project checkout has local changes"))
	case ProjectCheckoutRemoteMismatch:
		return projectErr(ProjectErrorRepositoryMismatch, errors.New("project checkout remote does not match"))
	default:
		return projectErr(ProjectErrorCheckoutUnsafe, errors.New("project checkout is unsafe"))
	}
}

func (r *ProjectRegistry) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (resolution ProjectResolution) SafeStatus() ProjectStatus {
	return ProjectStatus{
		Alias:      resolution.Project.Alias,
		Repository: resolution.Project.Owner + "/" + resolution.Project.Repository,
		Target:     resolution.TargetAlias,
		State:      "ready",
		Profile:    resolution.Workspace.Profile,
		Mode:       resolution.Workspace.Mode,
	}
}

func normalizeProjectProfiles(input []WorkspaceProfile) ([]WorkspaceProfile, error) {
	if len(input) == 0 || len(input) > maxProjectProfiles {
		return nil, projectErr(ProjectErrorInvalidInput, errors.New("project profiles are invalid"))
	}
	seen := map[WorkspaceProfile]bool{}
	profiles := make([]WorkspaceProfile, 0, len(input))
	for _, profile := range input {
		if profile != WorkspaceProfileSandbox && profile != WorkspaceProfileLinuxWorkcell || seen[profile] {
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
	return scanProject(scanner.QueryRow(`SELECT alias,owner,repository,preferred_target,created_at,updated_at FROM projects WHERE alias=?`, alias))
}

func loadProjectTx(tx *sql.Tx, alias string) (Project, bool, error) {
	return scanProject(tx.QueryRow(`SELECT alias,owner,repository,preferred_target,created_at,updated_at FROM projects WHERE alias=?`, alias))
}

func scanProject(row *sql.Row) (Project, bool, error) {
	var project Project
	var createdAt, updatedAt int64
	if err := row.Scan(&project.Alias, &project.Owner, &project.Repository, &project.PreferredTarget, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, false, nil
		}
		return Project{}, false, err
	}
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

func (r *ProjectRegistry) registrationMatchesTx(tx *sql.Tx, project Project, targetAlias, workspaceID string, profiles []WorkspaceProfile, owner, repository, preferredTarget string) (bool, error) {
	if project.Owner != owner || project.Repository != repository || project.PreferredTarget != preferredTarget {
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
