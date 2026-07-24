package workqueue

import (
	"testing"
	"time"
)

func TestCleanupTerminalHonorsTTLStateDependenciesAndLimit(t *testing.T) {
	store := openTestStore(t, Config{})
	now := time.Date(2026, 7, 24, 16, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	old, _, err := store.Enqueue(testSpec("cleanup-old-0001", "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseNext("vps.build", "worker-cleanup-0001", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(old.ID, lease.ID, lease.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}

	protected, _, err := store.Enqueue(testSpec("cleanup-root-0001", "beta"))
	if err != nil {
		t.Fatal(err)
	}
	protectedLease, err := store.LeaseNext("vps.build", "worker-cleanup-0002", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(protected.ID, protectedLease.ID, protectedLease.Fence, Result{Outcome: StateSucceeded, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	childSpec := testSpec("cleanup-child-0001", "beta")
	childSpec.Dependencies = []string{protected.ID}
	if _, _, err := store.Enqueue(childSpec); err != nil {
		t.Fatal(err)
	}

	now = now.Add(48 * time.Hour)
	queued, _, err := store.Enqueue(testSpec("cleanup-queued-0001", "gamma"))
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.CleanupTerminal(24*time.Hour, 1)
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if _, found, err := store.Get(old.ID); err != nil || found {
		t.Fatalf("old found=%v err=%v", found, err)
	}
	if _, found, err := store.Get(protected.ID); err != nil || !found {
		t.Fatalf("protected found=%v err=%v", found, err)
	}
	if _, found, err := store.Get(queued.ID); err != nil || !found {
		t.Fatalf("queued found=%v err=%v", found, err)
	}
	if _, err := store.CleanupTerminal(0, 1); err == nil {
		t.Fatal("zero retention accepted")
	}
	if _, err := store.CleanupTerminal(time.Hour, 0); err == nil {
		t.Fatal("zero limit accepted")
	}
}
