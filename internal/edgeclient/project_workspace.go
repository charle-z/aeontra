package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const directWorkspaceKeyPrefix = "direct-workspace:"

// PrepareDirectWorkspace reuses the managed worktree registry without a model
// runtime or another scheduler. A different caller key names a different writer.
func (m *ProjectWorktreeManager) PrepareDirectWorkspace(ctx context.Context, resolved ProjectResolution, base, key string) (ProjectWorktreeSnapshot, bool, error) {
	if !projectWorktreeIdempotencyPattern.MatchString(key) {
		return ProjectWorktreeSnapshot{}, false, ErrProjectWorktreeInvalid
	}
	sum := sha256.Sum256([]byte(resolved.Project.Alias + "\x00" + resolved.TargetAlias + "\x00" + key))
	digest := hex.EncodeToString(sum[:])
	return m.Create(ctx, ProjectWorktreeCreateRequest{
		Alias: resolved.Project.Alias, TargetAlias: resolved.TargetAlias,
		Repository:           resolved.Project.Owner + "/" + resolved.Project.Repository,
		CanonicalWorkspaceID: resolved.Workspace.ID, CanonicalPath: resolved.Workspace.Path,
		BaseCommit: base, Role: ProjectWorktreeWriter, JobID: "wj_" + digest[:32],
		LeaseID: "wl_" + digest[32:], Fence: 1, IdempotencyKey: directWorkspaceKeyPrefix + digest,
	})
}

// ResolveDirectWorkspace cannot select a model worker's leased checkout. The
// marker is private immutable registry data, not a caller-provided assertion.
func (m *ProjectWorktreeManager) ResolveDirectWorkspace(ctx context.Context, resolved ProjectResolution, id string) (ProjectResolution, error) {
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(id) {
		return ProjectResolution{}, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot, found, err := m.byID(id)
	if err != nil {
		return ProjectResolution{}, err
	}
	if !found || snapshot.State != ProjectWorktreeReady {
		return ProjectResolution{}, ErrProjectWorktreeNotFound
	}
	if !directWorkspaceMatches(snapshot, resolved) {
		return ProjectResolution{}, ErrProjectWorktreeConflict
	}
	if err := m.revalidate(ctx, snapshot); err != nil {
		return ProjectResolution{}, err
	}
	workspace, err := m.workspaces.Get(snapshot.WorkspaceID)
	if err != nil {
		return ProjectResolution{}, ErrProjectWorktreeUnsafe
	}
	resolved.Workspace = workspace
	return resolved, nil
}

func directWorkspaceMatches(snapshot ProjectWorktreeSnapshot, resolved ProjectResolution) bool {
	return strings.HasPrefix(snapshot.idempotencyKey, directWorkspaceKeyPrefix) && snapshot.Role == ProjectWorktreeWriter &&
		snapshot.Alias == resolved.Project.Alias && snapshot.TargetAlias == resolved.TargetAlias &&
		snapshot.Repository == resolved.Project.Owner+"/"+resolved.Project.Repository && snapshot.CanonicalWorkspaceID == resolved.Workspace.ID
}

func (m *ProjectWorktreeManager) StatusDirectWorkspace(ctx context.Context, resolved ProjectResolution, id string) (ProjectWorktreeSnapshot, error) {
	if m == nil || m.db == nil || !projectWorktreeIDPattern.MatchString(id) {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeInvalid
	}
	m.mu.Lock()
	snapshot, found, err := m.byID(id)
	m.mu.Unlock()
	if err != nil {
		return ProjectWorktreeSnapshot{}, err
	}
	if !found {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeNotFound
	}
	if !directWorkspaceMatches(snapshot, resolved) {
		return ProjectWorktreeSnapshot{}, ErrProjectWorktreeConflict
	}
	return m.Status(ctx, id)
}
