//go:build !windows

package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestModelRuntimePollersDoNotStarveAnotherRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	blocked := make(chan struct{})
	second := make(chan struct{}, 1)
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runModelRuntimePollers(ctx, 2, time.Second, func(ctx context.Context) (bool, error) {
			if calls.Add(1) == 1 {
				select {
				case <-blocked:
				case <-ctx.Done():
				}
				return true, nil
			}
			select {
			case second <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return true, nil
		})
	}()
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		cancel()
		close(blocked)
		<-done
		t.Fatal("a blocked runtime starved the next lease")
	}
	cancel()
	close(blocked)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollers did not stop after cancellation")
	}
}

func TestModelRuntimePollersRespectConcurrencyLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{}, 3)
	var active atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runModelRuntimePollers(ctx, 2, time.Second, func(ctx context.Context) (bool, error) {
			active.Add(1)
			defer active.Add(-1)
			entered <- struct{}{}
			<-ctx.Done()
			return false, ctx.Err()
		})
	}()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			cancel()
			<-done
			t.Fatal("expected both pollers to start")
		}
	}
	select {
	case <-entered:
		t.Fatal("more pollers started than the configured limit")
	case <-time.After(50 * time.Millisecond):
	}
	if got := active.Load(); got != 2 {
		t.Fatalf("active pollers=%d, want 2", got)
	}
	cancel()
	<-done
}

func TestModelRuntimePollersStopAllSlotsOnKillSwitch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runModelRuntimePollers(ctx, 2, time.Second, func(ctx context.Context) (bool, error) {
			if calls.Add(1) == 1 {
				return false, edgeclient.ErrKillSwitch
			}
			<-ctx.Done()
			return false, ctx.Err()
		})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("kill switch did not stop all model runtime pollers")
	}
}
