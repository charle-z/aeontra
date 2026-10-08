package main

import (
	"context"
	"sync"
	"time"
)

// controlOperationGate fences Edge-wide mutations from ordinary operations.
//
// A control loop has multiple workers, so a plain mutex would bring back
// head-of-line blocking for independent reads and project operations. Readers
// share the gate; bundle/update/rollback/repair operations take the exclusive
// side. Try* plus a context-aware backoff is intentional: waiting for an
// exclusive operation must not strand a leased operation without heartbeats.
type controlOperationGate struct {
	mu sync.RWMutex
}

func (gate *controlOperationGate) acquire(ctx context.Context, exclusive bool) bool {
	if gate == nil {
		return true
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if exclusive {
			if gate.mu.TryLock() {
				return true
			}
		} else if gate.mu.TryRLock() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (gate *controlOperationGate) release(exclusive bool) {
	if gate == nil {
		return
	}
	if exclusive {
		gate.mu.Unlock()
		return
	}
	gate.mu.RUnlock()
}
