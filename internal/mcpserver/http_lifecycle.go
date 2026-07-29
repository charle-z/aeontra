package mcpserver

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/charle-z/mcp-devbox/internal/observability"
)

// httpServerLifecycle owns only process-local HTTP serving state. It never stores
// credentials or durable user identity. A replacement process always starts with
// fresh readiness and drain state.
type httpDrainObserver func(observability.EventName, time.Duration, observability.Outcome, observability.ErrorClass)

type httpServerLifecycle struct {
	ready        atomic.Bool
	draining     atomic.Bool
	drainOnce    sync.Once
	drainEndOnce sync.Once
	drainStarted atomic.Int64
	drainDone    chan struct{}
	observer     httpDrainObserver
}

func newHTTPServerLifecycle() *httpServerLifecycle {
	lifecycle := &httpServerLifecycle{drainDone: make(chan struct{})}
	lifecycle.ready.Store(true)
	return lifecycle
}

func (l *httpServerLifecycle) WithObserver(observer httpDrainObserver) *httpServerLifecycle {
	if l != nil {
		l.observer = observer
	}
	return l
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
		started := time.Now().UTC()
		l.drainStarted.Store(started.UnixNano())
		l.draining.Store(true)
		l.ready.Store(false)
		close(l.drainDone)
		if l.observer != nil {
			l.observer(observability.EventServerDrainStart, 0, observability.OutcomeSuccess, observability.ErrorNone)
		}
	})
}

func (l *httpServerLifecycle) EndDrain(outcome observability.Outcome, errorClass observability.ErrorClass) {
	if l == nil {
		return
	}
	l.drainEndOnce.Do(func() {
		duration := time.Duration(0)
		if started := l.drainStarted.Load(); started > 0 {
			duration = time.Since(time.Unix(0, started))
		}
		if l.observer != nil {
			l.observer(observability.EventServerDrainEnd, duration, outcome, errorClass)
		}
	})
}
