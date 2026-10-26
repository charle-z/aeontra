//go:build !windows

package edgeclient

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type projectRuntimeGate struct {
	mu   sync.RWMutex
	refs int
}

var projectRuntimeGates = struct {
	sync.Mutex
	entries map[string]*projectRuntimeGate
}{entries: make(map[string]*projectRuntimeGate)}

// Ordinary users overlap; exact worktree cleanup excludes only this workspace.
// Durable users register before launching, so their journals protect the roots
// after this short start guard is released.
func acquireProjectRuntimeGate(ctx context.Context, stateRoot, workspaceID string, exclusive bool) (func(), error) {
	if !workspaceIDPattern.MatchString(workspaceID) {
		return nil, ErrProjectWorktreeInvalid
	}
	key := filepath.Clean(stateRoot) + "\x00" + workspaceID
	projectRuntimeGates.Lock()
	gate := projectRuntimeGates.entries[key]
	if gate == nil {
		gate = &projectRuntimeGate{}
		projectRuntimeGates.entries[key] = gate
	}
	gate.refs++
	projectRuntimeGates.Unlock()
	drop := func() {
		projectRuntimeGates.Lock()
		defer projectRuntimeGates.Unlock()
		gate.refs--
		if gate.refs == 0 {
			delete(projectRuntimeGates.entries, key)
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		locked := false
		if exclusive {
			locked = gate.mu.TryLock()
		} else {
			locked = gate.mu.TryRLock()
		}
		if locked {
			var once sync.Once
			return func() {
				once.Do(func() {
					if exclusive {
						gate.mu.Unlock()
					} else {
						gate.mu.RUnlock()
					}
					drop()
				})
			}, nil
		}
		select {
		case <-ctx.Done():
			drop()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Called only for a fenced, explicitly cleaned managed worktree, while holding
// its exclusive runtime gate. No registry, task evidence or branch is pruned.
func cleanupProjectWorktreeRuntime(ctx context.Context, stateRoot string, snapshot ProjectWorktreeSnapshot) error {
	workspace := Workspace{ID: snapshot.WorkspaceID, Path: snapshot.path}
	roots, _, err := projectRuntimeRootPaths(stateRoot, workspace)
	if err != nil {
		return ErrProjectWorktreeUnsafe
	}
	if err := validateProjectWorktreeRuntime(stateRoot, snapshot); err != nil {
		return err
	}
	retained, err := projectWorktreeRuntimeRetained(ctx, stateRoot, snapshot.WorkspaceID)
	if err != nil {
		return err
	}
	if retained {
		return nil
	}
	remove := []string{roots.Runtime, roots.Cache}
	if entries, err := os.ReadDir(roots.Artifacts); err == nil {
		if len(entries) == 0 {
			remove = append(remove, roots.Artifacts)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrProjectWorktreeUnavailable
	}
	// OpenRoot confines removal if a descendant is replaced by a symlink.
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		return ErrProjectWorktreeUnavailable
	}
	defer root.Close()
	for _, path := range remove {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(stateRoot, path)
		if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
			return ErrProjectWorktreeUnsafe
		}
		if err := root.RemoveAll(relative); err != nil {
			return ErrProjectWorktreeUnavailable
		}
	}
	return nil
}

func projectWorktreeRuntimeRetained(ctx context.Context, stateRoot, workspaceID string) (bool, error) {
	toolboxPath := filepath.Join(stateRoot, projectToolboxStateDirectory, workspaceID+".json")
	if err := rejectSymlinkPath(toolboxPath); err != nil {
		return false, ErrProjectWorktreeUnsafe
	}
	if info, err := os.Lstat(toolboxPath); err == nil {
		if !info.Mode().IsRegular() || !ownedByCurrentUIDPortable(info) {
			return false, ErrProjectWorktreeUnsafe
		}
		return true, nil // A retained toolbox also owns its harnesses and profiles.
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, ErrProjectWorktreeUnavailable
	}
	for _, journal := range []struct{ path, query string }{
		{filepath.Join(stateRoot, projectProcessDatabaseFile), `SELECT EXISTS(SELECT 1 FROM project_processes WHERE workspace_id=? AND state IN ('starting','running','stopping'))`},
		{filepath.Join(stateRoot, openCodeRuntimeJournalFile), `SELECT EXISTS(SELECT 1 FROM local_opencode_runtimes WHERE workspace_id=? AND state IN ('starting','running'))`},
		{filepath.Join(stateRoot, "project-browser", "browser.db"), `SELECT EXISTS(SELECT 1 FROM browser_sessions WHERE workspace_id=?)`},
	} {
		retained, err := projectRuntimeJournalRetains(ctx, journal.path, journal.query, workspaceID)
		if err != nil {
			return false, err
		}
		if retained {
			return true, nil
		}
	}
	return false, nil
}

func validateProjectWorktreeRuntime(stateRoot string, snapshot ProjectWorktreeSnapshot) error {
	roots, _, err := projectRuntimeRootPaths(stateRoot, Workspace{ID: snapshot.WorkspaceID, Path: snapshot.path})
	if err != nil || validatePrivateRoot(stateRoot) != nil {
		return ErrProjectWorktreeUnsafe
	}
	for _, path := range []string{stateRoot, roots.Runtime, roots.Cache, roots.Artifacts} {
		if err := rejectSymlinkPath(path); err != nil {
			return ErrProjectWorktreeUnsafe
		}
		for _, dir := range []string{filepath.Dir(path), path} {
			if path == stateRoot && dir != stateRoot {
				continue
			}
			info, err := os.Lstat(dir)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !info.IsDir() || !ownedByCurrentUIDPortable(info) || info.Mode().Perm()&0o077 != 0 {
				return ErrProjectWorktreeUnsafe
			}
		}
	}
	return nil
}

func projectRuntimeJournalRetains(ctx context.Context, path, query, workspaceID string) (bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, ErrProjectWorktreeUnavailable
	}
	if err := rejectSymlinkPath(path); err != nil {
		return false, ErrProjectWorktreeUnsafe
	}
	if err := requirePrivateRegularFile(path); err != nil {
		return false, ErrProjectWorktreeUnsafe
	}
	info, err := os.Lstat(path)
	if err != nil || !ownedByCurrentUIDPortable(info) {
		return false, ErrProjectWorktreeUnsafe
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return false, ErrProjectWorktreeUnavailable
	}
	defer db.Close()
	var retained bool
	if err := db.QueryRowContext(ctx, query, workspaceID).Scan(&retained); err != nil {
		return false, ErrProjectWorktreeUnavailable
	}
	return retained, nil
}
