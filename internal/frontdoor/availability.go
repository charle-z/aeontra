package frontdoor

import (
	"context"
	"errors"
	"time"
)

var (
	errBackendUnavailable  = errors.New("compatible backend is unavailable")
	errBackendIncompatible = errors.New("backend compatibility is incompatible")
)

func (f *FrontDoor) storeSnapshot(next *snapshot) {
	f.stateMu.Lock()
	f.state.Store(next)
	close(f.stateChanged)
	f.stateChanged = make(chan struct{})
	f.stateMu.Unlock()
}

func (f *FrontDoor) stateChangeChannel() <-chan struct{} {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	return f.stateChanged
}

func (f *FrontDoor) probeIfStale(parent context.Context) error {
	f.probeMu.Lock()
	defer f.probeMu.Unlock()
	state := f.state.Load()
	if state != nil && !state.CheckedAt.IsZero() {
		age := time.Since(state.CheckedAt)
		if age >= 0 && age <= readinessFreshness {
			if state.Ready {
				return nil
			}
			if state.Reason == "backend_incompatible" {
				return errBackendIncompatible
			}
			return errBackendUnavailable
		}
	}
	return f.probeLocked(parent)
}

func (f *FrontDoor) waitForBackend(ctx context.Context, heartbeat func() error) error {
	waited := false
	for {
		if err := f.probeIfStale(ctx); err == nil {
			if waited {
				f.admissionRecoveries.Add(1)
			}
			return nil
		}
		if !waited {
			f.admissionWaits.Add(1)
			waited = true
		}
		changed := f.stateChangeChannel()
		if heartbeat == nil {
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					f.admissionTimeouts.Add(1)
				}
				return ctx.Err()
			case <-changed:
			}
			continue
		}
		timer := time.NewTimer(sseWaitKeepalive)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				f.admissionTimeouts.Add(1)
			}
			return ctx.Err()
		case <-changed:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			if err := heartbeat(); err != nil {
				return err
			}
		}
	}
}
