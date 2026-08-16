package edgeclient

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	projectWorktreeRegistryFile = "project-worktrees.db"
	projectWorktreeRootName     = ".mcp-devbox-worktrees"
	projectWorktreeSchema       = 1
	MaxProjectWorktreeList      = 100
)

type ProjectWorktreeRole string
type ProjectWorktreeState string

const (
	ProjectWorktreeReader ProjectWorktreeRole = "reader"
	ProjectWorktreeWriter ProjectWorktreeRole = "writer"

	ProjectWorktreeReady   ProjectWorktreeState = "ready"
	ProjectWorktreeRemoved ProjectWorktreeState = "removed"
)

var (
	projectWorktreeIDPattern          = regexp.MustCompile(`^wt_[a-f0-9]{32}$`)
	projectWorktreeJobPattern         = regexp.MustCompile(`^wj_[a-f0-9]{32}$`)
	projectWorktreeLeasePattern       = regexp.MustCompile(`^wl_[a-f0-9]{32}$`)
	projectWorktreeCommitPattern      = regexp.MustCompile(`^[a-f0-9]{40}$`)
	projectWorktreeIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

	ErrProjectWorktreeInvalid     = errors.New("project worktree request is invalid")
	ErrProjectWorktreeNotFound    = errors.New("project worktree not found")
	ErrProjectWorktreeConflict    = errors.New("project worktree identity conflicts")
	ErrProjectWorktreeStaleFence  = errors.New("project worktree stale fence rejected")
	ErrProjectWorktreeBaseChanged = errors.New("project worktree base changed")
	ErrProjectWorktreeDirty       = errors.New("project worktree contains local changes")
	ErrProjectWorktreeUnsafe      = errors.New("project worktree state is unsafe")
	ErrProjectWorktreeUnavailable = errors.New("project worktree manager is unavailable")
)

type ProjectWorktreeManagerConfig struct {
	StateRoot  string
	Roots      WorkspaceRoots
	Workspaces *WorkspaceRegistry
	Runner     DevGitCommandRunner
	Credential GitHubCredential
}

type ProjectWorktreeCreateRequest struct {
	Alias                string
	TargetAlias          string
	Repository           string
	CanonicalWorkspaceID string
	CanonicalPath        string
	BaseCommit           string
	Role                 ProjectWorktreeRole
	JobID                string
	LeaseID              string
	Fence                uint64
	IdempotencyKey       string
}

type ProjectWorktreeClaimRequest struct {
	ID      string
	JobID   string
	LeaseID string
	Fence   uint64
}

type ProjectWorktreeCleanupRequest struct {
	ID             string
	JobID          string
	LeaseID        string
	Fence          uint64
	IdempotencyKey string
}

type ProjectWorktreeSnapshot struct {
	ID                   string               `json:"worktree_id"`
	Alias                string               `json:"alias"`
	TargetAlias          string               `json:"target"`
	Repository           string               `json:"repository"`
	CanonicalWorkspaceID string               `json:"-"`
	WorkspaceID          string               `json:"workspace_id,omitempty"`
	BaseCommit           string               `json:"base_commit"`
	Branch               string               `json:"branch"`
	Role                 ProjectWorktreeRole  `json:"role"`
	State                ProjectWorktreeState `json:"state"`
	JobID                string               `json:"job_id"`
	LeaseID              string               `json:"lease_id"`
	Fence                uint64               `json:"fence"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
	EvidenceKnown        bool                 `json:"evidence_known,omitempty"`
	HeadCommit           string               `json:"head_commit,omitempty"`
	Clean                bool                 `json:"clean,omitempty"`
	CommitsAheadBase     int                  `json:"commits_ahead_base,omitempty"`
	ChangedPathCount     int                  `json:"changed_path_count,omitempty"`
	path                 string
	idempotencyKey       string
	cleanupKey           string
}

type ProjectWorktreeManager struct {
	db         *sql.DB
	stateRoot  string
	root       string
	roots      WorkspaceRoots
	workspaces *WorkspaceRegistry
	runner     DevGitCommandRunner
	credential GitHubCredential
	now        func() time.Time
	newID      func() (string, error)
	mu         sync.Mutex
}

func OpenProjectWorktreeManager(config ProjectWorktreeManagerConfig) (*ProjectWorktreeManager, error) {
	stateRoot := filepath.Clean(strings.TrimSpace(config.StateRoot))
	roots, err := normalizeWorkspaceRoots(config.Roots)
	if err != nil || !filepath.IsAbs(stateRoot) || config.Workspaces == nil || config.Workspaces.db == nil || config.Runner == nil ||
		config.Credential.SchemaVersion != 1 || !githubOwnerPattern.MatchString(config.Credential.Owner) || !validGitHubToken(config.Credential.Token) {
		return nil, ErrProjectWorktreeInvalid
	}
	if err := preparePrivateRoot(stateRoot); err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	root := filepath.Join(roots.Dev, projectWorktreeRootName)
	if err := prepareProjectWorktreeRoot(root, roots.Dev); err != nil {
		return nil, err
	}
	path := filepath.Join(stateRoot, projectWorktreeRegistryFile)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUIDPortable(info) {
			return nil, ErrProjectWorktreeUnsafe
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrProjectWorktreeUnavailable
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	db.SetMaxOpenConns(1)
	manager := &ProjectWorktreeManager{
		db: db, stateRoot: stateRoot, root: root, roots: roots, workspaces: config.Workspaces,
		runner: config.Runner, credential: config.Credential, now: time.Now, newID: newProjectWorktreeID,
	}
	if err := manager.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, ErrProjectWorktreeUnavailable
	}
	return manager, nil
}

func (m *ProjectWorktreeManager) initialize() error {
	if m == nil || m.db == nil {
		return ErrProjectWorktreeUnavailable
	}
	var version int
	if err := m.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version > projectWorktreeSchema {
		return ErrProjectWorktreeUnsafe
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=DELETE`,
		`PRAGMA synchronous=FULL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA max_page_count=4096`,
		`CREATE TABLE IF NOT EXISTS project_worktrees(
			worktree_id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			alias TEXT NOT NULL,
			target_alias TEXT NOT NULL,
			repository TEXT NOT NULL,
			canonical_workspace_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL UNIQUE,
			path TEXT NOT NULL UNIQUE,
			base_commit TEXT NOT NULL,
			branch TEXT NOT NULL,
			role TEXT NOT NULL,
			state TEXT NOT NULL,
			job_id TEXT NOT NULL,
			lease_id TEXT NOT NULL,
			fence INTEGER NOT NULL,
			cleanup_key TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS project_worktrees_project ON project_worktrees(alias,target_alias,created_at,worktree_id)`,
	} {
		if _, err := m.db.Exec(statement); err != nil {
			return ErrProjectWorktreeUnavailable
		}
	}
	if version == 0 {
		if _, err := m.db.Exec(`PRAGMA user_version=1`); err != nil {
			return ErrProjectWorktreeUnavailable
		}
	}
	return nil
}

func (m *ProjectWorktreeManager) Create(ctx context.Context, request ProjectWorktreeCreateRequest) (ProjectWorktreeSnapshot, bool, error) {
	request, err := m.normalizeCreateRequest(request)
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, found, err := m.byIdempotency(request.IdempotencyKey); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	} else if found {
		if !projectWorktreeCreateMatches(existing, request) {
			return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeConflict
		}
		if err := m.revalidate(ctx, existing); err != nil {
			return ProjectWorktreeSnapshot{}, false, err
		}
		return existing, true, nil
	}
	canonical, err := m.workspaces.Get(request.CanonicalWorkspaceID)
	if err != nil || canonical.Path != request.CanonicalPath || canonical.Profile != WorkspaceProfileLinuxWorkcell || canonical.Mode != WorkspaceModeDev {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeInvalid
	}
	if err := m.verifyCanonicalBase(ctx, request.CanonicalPath, request.BaseCommit); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	id, err := m.newID()
	if err != nil || !projectWorktreeIDPattern.MatchString(id) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	parent := filepath.Join(m.root, request.Alias)
	if err := prepareProjectWorktreeRoot(parent, m.root); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	target := filepath.Join(parent, id)
	branch := "codex/worktree-" + strings.TrimPrefix(id, "wt_")
	if filepath.Dir(target) != parent || !pathInside(m.root, target) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnsafe
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeConflict
	}
	if _, err := m.runner.Run(ctx, request.CanonicalPath, []string{"worktree", "add", "-b", branch, "--", target, request.BaseCommit}, m.credential); err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	rollback := true
	defer func() {
		if rollback {
			_, _ = m.runner.Run(context.Background(), request.CanonicalPath, []string{"worktree", "remove", "--force", "--", target}, m.credential)
		}
	}()
	if err := m.validatePhysical(ctx, target, request.CanonicalPath, request.BaseCommit, branch); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	workspace, created, err := m.workspaces.AddProfile(target, WorkspaceProfileLinuxWorkcell)
	if err != nil || !created {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	removeWorkspace := true
	defer func() {
		if removeWorkspace {
			_ = m.workspaces.Remove(workspace.ID)
		}
	}()
	now := m.now().UTC()
	snapshot := ProjectWorktreeSnapshot{
		ID: id, Alias: request.Alias, TargetAlias: request.TargetAlias, Repository: request.Repository,
		CanonicalWorkspaceID: request.CanonicalWorkspaceID, WorkspaceID: workspace.ID, BaseCommit: request.BaseCommit, Branch: branch,
		Role: request.Role, State: ProjectWorktreeReady, JobID: request.JobID, LeaseID: request.LeaseID, Fence: request.Fence,
		CreatedAt: now, UpdatedAt: now, path: target, idempotencyKey: request.IdempotencyKey,
	}
	if err := m.insert(snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	removeWorkspace = false
	rollback = false
	return snapshot, false, nil
}

func (m *ProjectWorktreeManager) Claim(request ProjectWorktreeClaimRequest) (ProjectWorktreeSnapshot, error) {
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(request.ID) || !projectWorktreeJobPattern.MatchString(request.JobID) || !projectWorktreeLeasePattern.MatchString(request.LeaseID) || request.Fence == 0 {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, found, err := m.byID(request.ID)
	if err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if !found || snapshot.State != ProjectWorktreeReady {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeNotFound
	}
	if snapshot.JobID != request.JobID {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeConflict
	}
	if snapshot.Fence == request.Fence && snapshot.LeaseID == request.LeaseID {
		return snapshot, nil
	}
	if request.Fence <= snapshot.Fence {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeStaleFence
	}
	now := m.now().UTC()
	result, err := m.db.Exec(`UPDATE project_worktrees SET lease_id=?,fence=?,updated_at=? WHERE worktree_id=? AND job_id=? AND fence=? AND state=?`, request.LeaseID, request.Fence, now.UnixNano(), request.ID, request.JobID, snapshot.Fence, ProjectWorktreeReady)
	if err != nil {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeUnavailable
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeStaleFence
	}
	snapshot.LeaseID, snapshot.Fence, snapshot.UpdatedAt = request.LeaseID, request.Fence, now
	return snapshot, nil
}

func (m *ProjectWorktreeManager) Status(ctx context.Context, id string) (ProjectWorktreeSnapshot, error) {
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(id) {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, found, err := m.byID(id)
	if err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if !found {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeNotFound
	}
	if snapshot.State == ProjectWorktreeReady {
		if err := m.revalidate(ctx, snapshot); err != nil {
			return ProjectWorktreeSnapshot{}, err
		}
		if err := m.collectEvidence(ctx, &snapshot); err != nil {
			return ProjectWorktreeSnapshot{}, err
		}
	}
	return snapshot, nil
}

func (m *ProjectWorktreeManager) collectEvidence(ctx context.Context, snapshot *ProjectWorktreeSnapshot) error {
	headText, err := m.runner.Run(ctx, snapshot.path, []string{"rev-parse", "--verify", "HEAD"}, m.credential)
	head := strings.TrimSpace(headText)
	if err != nil || !projectWorktreeCommitPattern.MatchString(head) {
		return ErrProjectWorktreeUnavailable
	}
	if _, err := m.runner.Run(ctx, snapshot.path, []string{"merge-base", "--is-ancestor", snapshot.BaseCommit, head}, m.credential); err != nil {
		return ErrProjectWorktreeBaseChanged
	}
	status, err := m.runner.Run(ctx, snapshot.path, []string{"status", "--porcelain=v1", "--untracked-files=all"}, m.credential)
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	aheadText, err := m.runner.Run(ctx, snapshot.path, []string{"rev-list", "--count", "--max-count=10001", snapshot.BaseCommit + ".." + head}, m.credential)
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(aheadText))
	if err != nil || ahead < 0 || ahead > 10000 || ((head == snapshot.BaseCommit) != (ahead == 0)) {
		return ErrProjectWorktreeUnsafe
	}
	changed, err := m.runner.Run(ctx, snapshot.path, []string{"diff", "--name-only", "-z", snapshot.BaseCommit + ".." + head}, m.credential)
	if err != nil || (changed != "" && !strings.HasSuffix(changed, "\x00")) {
		return ErrProjectWorktreeUnavailable
	}
	changedPaths := strings.Count(changed, "\x00")
	if changedPaths > 10000 {
		return ErrProjectWorktreeUnsafe
	}
	snapshot.EvidenceKnown = true
	snapshot.HeadCommit = head
	snapshot.Clean = ProjectCheckoutStatusClean(status)
	snapshot.CommitsAheadBase = ahead
	snapshot.ChangedPathCount = changedPaths
	return nil
}

func (m *ProjectWorktreeManager) List(ctx context.Context, alias, target string, limit int) ([]ProjectWorktreeSnapshot, error) {
	alias = strings.ToLower(strings.TrimSpace(alias))
	target = strings.ToLower(strings.TrimSpace(target))
	if m == nil || m.db == nil || !projectAliasPattern.MatchString(alias) || !projectTargetPattern.MatchString(target) || limit < 1 || limit > MaxProjectWorktreeList {
		return nil, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := m.db.Query(projectWorktreeSelect+` WHERE alias=? AND target_alias=? ORDER BY created_at,worktree_id LIMIT ?`, alias, target, limit)
	if err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	defer rows.Close()
	items := make([]ProjectWorktreeSnapshot, 0)
	for rows.Next() {
		item, err := scanProjectWorktree(rows)
		if err != nil {
			return nil, ErrProjectWorktreeUnavailable
		}
		if item.State == ProjectWorktreeReady {
			if err := m.revalidate(ctx, item); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, ErrProjectWorktreeUnavailable
	}
	return items, nil
}

func (m *ProjectWorktreeManager) Cleanup(ctx context.Context, request ProjectWorktreeCleanupRequest) (ProjectWorktreeSnapshot, bool, error) {
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(request.ID) || !projectWorktreeJobPattern.MatchString(request.JobID) || !projectWorktreeLeasePattern.MatchString(request.LeaseID) || request.Fence == 0 || !projectWorktreeIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, found, err := m.byID(request.ID)
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	if !found {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeNotFound
	}
	if snapshot.JobID != request.JobID || snapshot.LeaseID != request.LeaseID || snapshot.Fence != request.Fence {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeStaleFence
	}
	if snapshot.State == ProjectWorktreeRemoved {
		if snapshot.cleanupKey != request.IdempotencyKey {
			return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeConflict
		}
		return snapshot, true, nil
	}
	if _, pathErr := os.Lstat(snapshot.path); errors.Is(pathErr, os.ErrNotExist) {
		if removeErr := m.workspaces.Remove(snapshot.WorkspaceID); removeErr != nil && !strings.Contains(removeErr.Error(), "not found") {
			return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
		}
		return m.finishCleanup(snapshot, request.IdempotencyKey)
	} else if pathErr != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	if err := m.revalidate(ctx, snapshot); err != nil {
		return ProjectWorktreeSnapshot{}, false, err
	}
	status, err := m.runner.Run(ctx, snapshot.path, []string{"status", "--porcelain=v1", "--untracked-files=all"}, m.credential)
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	if !ProjectCheckoutStatusClean(status) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeDirty
	}
	canonical, err := m.workspaces.Get(snapshot.CanonicalWorkspaceID)
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnsafe
	}
	if _, err := m.runner.Run(ctx, canonical.Path, []string{"worktree", "remove", "--force", "--", snapshot.path}, m.credential); err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	if err := m.workspaces.Remove(snapshot.WorkspaceID); err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	return m.finishCleanup(snapshot, request.IdempotencyKey)
}

func (m *ProjectWorktreeManager) finishCleanup(snapshot ProjectWorktreeSnapshot, cleanupKey string) (ProjectWorktreeSnapshot, bool, error) {
	now := m.now().UTC()
	result, err := m.db.Exec(`UPDATE project_worktrees SET state=?,cleanup_key=?,updated_at=? WHERE worktree_id=? AND state=? AND lease_id=? AND fence=?`, ProjectWorktreeRemoved, cleanupKey, now.UnixNano(), snapshot.ID, ProjectWorktreeReady, snapshot.LeaseID, snapshot.Fence)
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeStaleFence
	}
	snapshot.State, snapshot.cleanupKey, snapshot.UpdatedAt = ProjectWorktreeRemoved, cleanupKey, now
	return snapshot, false, nil
}

func (m *ProjectWorktreeManager) Close() error {
	if m == nil || m.db == nil {
		return nil
	}
	return m.db.Close()
}

func (m *ProjectWorktreeManager) normalizeCreateRequest(request ProjectWorktreeCreateRequest) (ProjectWorktreeCreateRequest, error) {
	if m == nil || m.db == nil {
		return ProjectWorktreeCreateRequest{}, ErrProjectWorktreeUnavailable
	}
	request.Alias = strings.ToLower(strings.TrimSpace(request.Alias))
	request.TargetAlias = strings.ToLower(strings.TrimSpace(request.TargetAlias))
	request.Repository = strings.ToLower(strings.TrimSpace(request.Repository))
	request.CanonicalPath = filepath.Clean(strings.TrimSpace(request.CanonicalPath))
	request.BaseCommit = strings.ToLower(strings.TrimSpace(request.BaseCommit))
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	parts := strings.Split(request.Repository, "/")
	if len(parts) != 2 || !projectAliasPattern.MatchString(request.Alias) || !projectTargetPattern.MatchString(request.TargetAlias) ||
		!githubOwnerPattern.MatchString(parts[0]) || !devGitSimplePattern.MatchString(parts[1]) || !strings.EqualFold(parts[0], m.credential.Owner) ||
		!workspaceIDPattern.MatchString(request.CanonicalWorkspaceID) || !filepath.IsAbs(request.CanonicalPath) || !pathInside(m.roots.Dev, request.CanonicalPath) ||
		!projectWorktreeCommitPattern.MatchString(request.BaseCommit) || (request.Role != ProjectWorktreeReader && request.Role != ProjectWorktreeWriter) ||
		!projectWorktreeJobPattern.MatchString(request.JobID) || !projectWorktreeLeasePattern.MatchString(request.LeaseID) || request.Fence == 0 || !projectWorktreeIdempotencyPattern.MatchString(request.IdempotencyKey) {
		return ProjectWorktreeCreateRequest{}, ErrProjectWorktreeInvalid
	}
	return request, nil
}

func (m *ProjectWorktreeManager) verifyCanonicalBase(ctx context.Context, canonicalPath, base string) error {
	info, err := os.Lstat(filepath.Join(canonicalPath, ".git"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrProjectWorktreeUnsafe
	}
	output, err := m.runner.Run(ctx, canonicalPath, []string{"rev-parse", "--verify", "HEAD"}, m.credential)
	if err != nil || strings.TrimSpace(output) != base {
		return ErrProjectWorktreeBaseChanged
	}
	return nil
}

func (m *ProjectWorktreeManager) validatePhysical(ctx context.Context, target, canonicalPath, base, branch string) error {
	validated, err := validateLinuxWorkcellPath(target, m.roots)
	if err != nil || validated != filepath.Clean(target) {
		return ErrProjectWorktreeUnsafe
	}
	gitFile := filepath.Join(target, ".git")
	info, err := os.Lstat(gitFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || info.Size() < 10 || info.Size() > 4096 {
		return ErrProjectWorktreeUnsafe
	}
	body, err := os.ReadFile(gitFile)
	if err != nil || !strings.HasPrefix(string(body), "gitdir: ") {
		return ErrProjectWorktreeUnsafe
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(string(body), "gitdir: "))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(target, gitDir)
	}
	gitDir = filepath.Clean(gitDir)
	commonRoot := filepath.Join(canonicalPath, ".git", "worktrees")
	if !pathInside(commonRoot, gitDir) || filepath.Clean(gitDir) == filepath.Clean(commonRoot) || rejectSymlinkPath(gitDir) != nil {
		return ErrProjectWorktreeUnsafe
	}
	head, err := m.runner.Run(ctx, target, []string{"rev-parse", "--verify", "HEAD"}, m.credential)
	currentHead := strings.TrimSpace(head)
	if err != nil || !projectWorktreeCommitPattern.MatchString(currentHead) || (base != "" && currentHead != base) {
		return ErrProjectWorktreeBaseChanged
	}
	currentBranch, err := m.runner.Run(ctx, target, []string{"branch", "--show-current"}, m.credential)
	if err != nil || strings.TrimSpace(currentBranch) != branch {
		return ErrProjectWorktreeUnsafe
	}
	top, err := m.runner.Run(ctx, target, []string{"rev-parse", "--show-toplevel"}, m.credential)
	if err != nil || filepath.Clean(strings.TrimSpace(top)) != filepath.Clean(target) {
		return ErrProjectWorktreeUnsafe
	}
	return nil
}

func (m *ProjectWorktreeManager) revalidate(ctx context.Context, snapshot ProjectWorktreeSnapshot) error {
	if snapshot.State != ProjectWorktreeReady || !pathInside(m.root, snapshot.path) || filepath.Base(snapshot.path) != snapshot.ID || filepath.Base(filepath.Dir(snapshot.path)) != snapshot.Alias {
		return ErrProjectWorktreeUnsafe
	}
	canonical, err := m.workspaces.Get(snapshot.CanonicalWorkspaceID)
	if err != nil {
		return ErrProjectWorktreeUnsafe
	}
	workspace, err := m.workspaces.Get(snapshot.WorkspaceID)
	if err != nil || workspace.Path != snapshot.path || workspace.Profile != WorkspaceProfileLinuxWorkcell || workspace.Mode != WorkspaceModeDev {
		return ErrProjectWorktreeUnsafe
	}
	return m.validatePhysical(ctx, snapshot.path, canonical.Path, "", snapshot.Branch)
}

func prepareProjectWorktreeRoot(path, parent string) error {
	path, parent = filepath.Clean(path), filepath.Clean(parent)
	if !filepath.IsAbs(path) || !filepath.IsAbs(parent) || (filepath.Dir(path) != parent && !pathInside(parent, path)) || path == parent {
		return ErrProjectWorktreeUnsafe
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 || !ownedByCurrentUIDPortable(info) {
		return ErrProjectWorktreeUnsafe
	}
	if err := rejectSymlinkPath(parent); err != nil {
		return ErrProjectWorktreeUnsafe
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || !ownedByCurrentUIDPortable(info) {
			return ErrProjectWorktreeUnsafe
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrProjectWorktreeUnavailable
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return ErrProjectWorktreeUnavailable
	}
	return nil
}

func newProjectWorktreeID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "wt_" + hex.EncodeToString(raw), nil
}

func projectWorktreeCreateMatches(snapshot ProjectWorktreeSnapshot, request ProjectWorktreeCreateRequest) bool {
	return snapshot.Alias == request.Alias && snapshot.TargetAlias == request.TargetAlias && snapshot.Repository == request.Repository &&
		snapshot.CanonicalWorkspaceID == request.CanonicalWorkspaceID && snapshot.BaseCommit == request.BaseCommit && snapshot.Role == request.Role &&
		snapshot.JobID == request.JobID && snapshot.LeaseID == request.LeaseID && snapshot.Fence == request.Fence && snapshot.State == ProjectWorktreeReady
}

const projectWorktreeSelect = `SELECT worktree_id,idempotency_key,alias,target_alias,repository,canonical_workspace_id,workspace_id,path,base_commit,branch,role,state,job_id,lease_id,fence,cleanup_key,created_at,updated_at FROM project_worktrees`

func (m *ProjectWorktreeManager) insert(snapshot ProjectWorktreeSnapshot) error {
	_, err := m.db.Exec(`INSERT INTO project_worktrees(worktree_id,idempotency_key,alias,target_alias,repository,canonical_workspace_id,workspace_id,path,base_commit,branch,role,state,job_id,lease_id,fence,cleanup_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		snapshot.ID, snapshot.idempotencyKey, snapshot.Alias, snapshot.TargetAlias, snapshot.Repository, snapshot.CanonicalWorkspaceID, snapshot.WorkspaceID, snapshot.path,
		snapshot.BaseCommit, snapshot.Branch, snapshot.Role, snapshot.State, snapshot.JobID, snapshot.LeaseID, snapshot.Fence, "", snapshot.CreatedAt.UnixNano(), snapshot.UpdatedAt.UnixNano())
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	return nil
}

func (m *ProjectWorktreeManager) byID(id string) (ProjectWorktreeSnapshot, bool, error) {
	snapshot, err := scanProjectWorktree(m.db.QueryRow(projectWorktreeSelect+` WHERE worktree_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectWorktreeSnapshot{}, false, nil
	}
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	return snapshot, true, nil
}

func (m *ProjectWorktreeManager) byIdempotency(key string) (ProjectWorktreeSnapshot, bool, error) {
	snapshot, err := scanProjectWorktree(m.db.QueryRow(projectWorktreeSelect+` WHERE idempotency_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectWorktreeSnapshot{}, false, nil
	}
	if err != nil {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeUnavailable
	}
	return snapshot, true, nil
}

func scanProjectWorktree(scanner interface{ Scan(...any) error }) (ProjectWorktreeSnapshot, error) {
	var snapshot ProjectWorktreeSnapshot
	var createdAt, updatedAt int64
	err := scanner.Scan(&snapshot.ID, &snapshot.idempotencyKey, &snapshot.Alias, &snapshot.TargetAlias, &snapshot.Repository,
		&snapshot.CanonicalWorkspaceID, &snapshot.WorkspaceID, &snapshot.path, &snapshot.BaseCommit, &snapshot.Branch, &snapshot.Role, &snapshot.State,
		&snapshot.JobID, &snapshot.LeaseID, &snapshot.Fence, &snapshot.cleanupKey, &createdAt, &updatedAt)
	if err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	snapshot.CreatedAt, snapshot.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
	if !validProjectWorktreeSnapshot(snapshot) {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeUnsafe
	}
	return snapshot, nil
}

func validProjectWorktreeSnapshot(snapshot ProjectWorktreeSnapshot) bool {
	parts := strings.Split(snapshot.Repository, "/")
	return projectWorktreeIDPattern.MatchString(snapshot.ID) && projectAliasPattern.MatchString(snapshot.Alias) && projectTargetPattern.MatchString(snapshot.TargetAlias) &&
		len(parts) == 2 && githubOwnerPattern.MatchString(parts[0]) && devGitSimplePattern.MatchString(parts[1]) && workspaceIDPattern.MatchString(snapshot.CanonicalWorkspaceID) &&
		workspaceIDPattern.MatchString(snapshot.WorkspaceID) && filepath.IsAbs(snapshot.path) && projectWorktreeCommitPattern.MatchString(snapshot.BaseCommit) && validDevGitBranch(snapshot.Branch) && strings.HasPrefix(snapshot.Branch, "codex/worktree-") &&
		(snapshot.Role == ProjectWorktreeReader || snapshot.Role == ProjectWorktreeWriter) && (snapshot.State == ProjectWorktreeReady || snapshot.State == ProjectWorktreeRemoved) &&
		projectWorktreeJobPattern.MatchString(snapshot.JobID) && projectWorktreeLeasePattern.MatchString(snapshot.LeaseID) && snapshot.Fence > 0 &&
		projectWorktreeIdempotencyPattern.MatchString(snapshot.idempotencyKey) && (snapshot.cleanupKey == "" || projectWorktreeIdempotencyPattern.MatchString(snapshot.cleanupKey)) &&
		!snapshot.CreatedAt.IsZero() && !snapshot.UpdatedAt.Before(snapshot.CreatedAt)
}

// Stable ordering is useful to callers that combine several independently leased workers.
func SortProjectWorktrees(items []ProjectWorktreeSnapshot) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}
