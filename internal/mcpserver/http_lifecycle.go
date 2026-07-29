package mcpserver

import (
	"sync"
	"sync/atomic"
)

// httpServerLifecycle owns only process-local HTTP serving state. It never stores
// credentials or durable user identity. A replacement process always starts with
// fresh readiness and drain state.
type httpServerLifecycle struct {
	ready     atomic.Bool
	draining  atomic.Bool
	drainOnce sync.Once
	drainDone chan struct{}
}

func newHTTPServerLifecycle() *httpServerLifecycle {
	lifecycle := &httpServerLifecycle{drainDone: make(chan struct{})}
	lifecycle.ready.Store(true)
	return lifecycle
}

func (l *httpServerLifecycle) Ready() bool {
	return l != nil && l.ready.Load()
}

func (l *httpServerLifecycle) Draining() bool {
	return l != nil && l.draining.Load()
}

func (l *httpServerLifecycle) DrainDone() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.drainDone
}

// BeginDrain is idempotent. It rejects new initialization first, then drops
// readiness and wakes long-lived streams before listener shutdown starts.
func (l *httpServerLifecycle) BeginDrain() {
	if l == nil {
		return
	}
	l.drainOnce.Do(func() {
		l.draining.Store(true)
		l.ready.Store(false)
		close(l.drainDone)
	})
}
