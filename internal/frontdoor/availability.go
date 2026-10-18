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

func (f *FrontDoor) probeAfter(parent context.Context, minimum time.Time) error {
	f.probeMu.Lock()
	defer f.probeMu.Unlock()
	state := f.state.Load()
	if state != nil && !state.CheckedAt.Before(minimum) {
		if state.Ready {
			return nil
		}
		if state.Reason == "backend_incompatible" {
			return errBackendIncompatible
		}
		return errBackendUnavailable
	}
	return f.probeLocked(parent)
}

func (f *FrontDoor) waitForBackend(ctx context.Context, heartbeat func() error) error {
	minimum := time.Now().UTC()
	waited := false
	for {
		changed := f.stateChangeChannel()
		err := f.probeAfter(ctx, minimum)
		if errors.Is(err, errBackendIncompatible) {
			return err
		}
		if err == nil {
			if waited {
				f.admissionRecoveries.Add(1)
			}
			return nil
		}
		if !waited {
			f.admissionWaits.Add(1)
			waited = true
		}
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
			stopTimer(timer)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				f.admissionTimeouts.Add(1)
			}
			return ctx.Err()
		case <-changed:
			stopTimer(timer)
		case <-timer.C:
			if err := heartbeat(); err != nil {
				return err
			}
		}
	}
}
