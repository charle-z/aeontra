package modelturn

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCleanupIfDueThrottlesPollingWrites(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)}
	store := openWaitStore(t, filepath.Join(t.TempDir(), "turns"), clock.Now)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request := validRequest(1)
	request.RuntimeID = runtimeRecord.RuntimeID
	request.TTL = 500 * time.Millisecond
	turn, err := store.CreateTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	clock.Add(750 * time.Millisecond)
	var wait sync.WaitGroup
	for range 64 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := store.cleanupIfDue(context.Background()); err != nil {
				t.Errorf("cleanup before interval: %v", err)
			}
		}()
	}
	wait.Wait()
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusAwaitingModel {
		t.Fatalf("cleanup ran before interval: %s", record.Status)
	}

	clock.Add(250 * time.Millisecond)
	if err := store.cleanupIfDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, err = store.Get(context.Background(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusExpired {
		t.Fatalf("due cleanup did not expire turn: %s", record.Status)
	}
}
