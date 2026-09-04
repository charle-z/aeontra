//go:build !windows

package edgeclient

import (
	"context"
	"sync"
)

// ProjectToolboxManager instances are intentionally short-lived: the Edge
// opens one for each control operation. The mutex on ProjectToolboxManager is
// therefore only an intra-instance guard and cannot protect the durable
// record when two operations target the same workspace concurrently.
//
// Keep the lock keyed by the validated state root and workspace ID so all
// managers in this Edge process serialize read-modify-write and container
// lifecycle operations for one toolbox, while different workspaces remain
// independent. The Edge itself is a single managed process; its process
// lifecycle is the boundary for this in-memory coordination state.
type projectToolboxWorkspaceLock struct {
	token chan struct{}
	refs  int
}

type projectToolboxWorkspaceLockRegistry struct {
	mu      sync.Mutex
	entries map[string]*projectToolboxWorkspaceLock
}

var projectToolboxWorkspaceLocks = projectToolboxWorkspaceLockRegistry{entries: make(map[string]*projectToolboxWorkspaceLock)}

func (manager *ProjectToolboxManager) acquireWorkspaceLock(ctx context.Context, workspaceID string) (func(), error) {
	if manager == nil || !workspaceIDPattern.MatchString(workspaceID) {
		return nil, ErrProjectToolboxUnsafeState
	}
	key := manager.stateRoot + "\x00" + workspaceID
	projectToolboxWorkspaceLocks.mu.Lock()
	lock := projectToolboxWorkspaceLocks.entries[key]
	if lock == nil {
		lock = &projectToolboxWorkspaceLock{token: make(chan struct{}, 1)}
		projectToolboxWorkspaceLocks.entries[key] = lock
	}
	lock.refs++
	projectToolboxWorkspaceLocks.mu.Unlock()

	removeReference := func() {
		projectToolboxWorkspaceLocks.mu.Lock()
		defer projectToolboxWorkspaceLocks.mu.Unlock()
		if lock.refs > 0 {
			lock.refs--
		}
		if lock.refs == 0 && projectToolboxWorkspaceLocks.entries[key] == lock {
			delete(projectToolboxWorkspaceLocks.entries, key)
		}
	}
	select {
	case lock.token <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-lock.token
				removeReference()
			})
		}, nil
	case <-ctx.Done():
		removeReference()
		return nil, ctx.Err()
	}
}
