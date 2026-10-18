package modelturn

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitLeaseNextRuntimeDoesNotWakeItself(t *testing.T) {
	var clockCalls atomic.Int64
	now := func() time.Time {
		clockCalls.Add(1)
		return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	}
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), now)
	baseline := clockCalls.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, found, err := store.WaitLeaseNextRuntime(ctx, "ed_ffffffffffffffffffffffffffffffff")
	if !errors.Is(err, context.DeadlineExceeded) || found {
		t.Fatalf("wait result = found %v, err %v", found, err)
	}
	if attempts := clockCalls.Load() - baseline; attempts > 2 {
		t.Fatalf("idle lease waiter retried %d times before its one-second fallback", attempts)
	}
}
