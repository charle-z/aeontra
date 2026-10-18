package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitNextAfterReturnsAvailableTurnImmediately(t *testing.T) {
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), nil)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"ready"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	offer, pending, current, err := store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
	if err != nil || !pending || offer.TurnID != created.ID || current.LastSequence != 1 {
		t.Fatalf("offer=%+v pending=%t runtime=%+v err=%v", offer, pending, current, err)
	}
}

func TestWaitNextAfterWakesAcrossStoreConnections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "turns")
	reader := openWaitStore(t, root, nil)
	writer := openWaitStore(t, root, nil)
	runtimeRecord, err := reader.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		offer   Offer
		pending bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		offer, pending, _, err := reader.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
		done <- result{offer: offer, pending: pending, err: err}
	}()
	time.Sleep(30 * time.Millisecond)
	created, err := writer.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"wake"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !got.pending || got.offer.TurnID != created.ID {
			t.Fatalf("result=%+v created=%+v", got, created)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter was not notified")
	}
}

func TestWaitNextAfterTimeoutAndCancellation(t *testing.T) {
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), nil)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, pending, _, err := store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0); !errors.Is(err, context.DeadlineExceeded) || pending {
		t.Fatalf("timeout pending=%t err=%v", pending, err)
	}

	cancelCtx, cancelNow := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, _, err := store.WaitNextAfter(cancelCtx, runtimeRecord.RuntimeID, 0)
		done <- err
	}()
	cancelNow()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled wait did not return")
	}
}

func TestWaitNextAfterReturnsTerminalAndDisconnectedState(t *testing.T) {
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), nil)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"disconnect"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDisconnected(context.Background(), turn.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, pending, current, err := store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 1); err != nil || pending || current.ActiveTurnStatus != StatusDisconnected {
		t.Fatalf("disconnected pending=%t runtime=%+v err=%v", pending, current, err)
	}
	if err := store.CancelRuntime(context.Background(), runtimeRecord.RuntimeID); err != nil {
		t.Fatal(err)
	}
	if _, pending, current, err := store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 1); err != nil || pending || current.Status != RuntimeCancelled {
		t.Fatalf("cancelled pending=%t runtime=%+v err=%v", pending, current, err)
	}
}

func TestWaitNextAfterBroadcastsToConcurrentWaiters(t *testing.T) {
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), nil)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const waiters = 8
	ready := make(chan struct{}, waiters)
	errs := make(chan error, waiters)
	var wg sync.WaitGroup
	for range waiters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready <- struct{}{}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			offer, pending, _, err := store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
			if err != nil {
				errs <- err
				return
			}
			if !pending || offer.Sequence != 1 {
				errs <- ErrTurnConflict
			}
		}()
	}
	for range waiters {
		<-ready
	}
	if _, err := store.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"broadcast"}`),
	}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestWaitNextAfterDoesNotBusyPoll(t *testing.T) {
	var nowCalls atomic.Int64
	now := func() time.Time {
		nowCalls.Add(1)
		return time.Now()
	}
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), now)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeGoroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _, _, err = store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait err=%v", err)
	}
	if calls := nowCalls.Load(); calls > 8 {
		t.Fatalf("wait performed too many time checks: %d", calls)
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > beforeGoroutines+1 {
		t.Fatalf("possible goroutine leak: before=%d after=%d", beforeGoroutines, after)
	}
}

func TestWaitNextAfterCanRetryAfterStoreReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "turns")
	store := openWaitStore(t, root, nil)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	_, _, _, err = store.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first wait err=%v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	created, err := reopened.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"after restart"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	offer, pending, _, err := reopened.WaitNextAfter(ctx, runtimeRecord.RuntimeID, 0)
	if err != nil || !pending || offer.TurnID != created.ID {
		t.Fatalf("offer=%+v pending=%t err=%v", offer, pending, err)
	}
}

func openWaitStore(t *testing.T, root string, now func() time.Time) *Store {
	t.Helper()
	store, err := OpenStore(StoreConfig{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
