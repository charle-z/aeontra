package modelturn

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func continuationRequest(t *testing.T, store *Store, workspaceID, key string, ttl time.Duration) BoundRuntimeRequest {
	t.Helper()
	goal := []byte("Resume the registered workspace using its local trusted contract and persistent checkpoint. Perform only operations authorized by the local contract. Keep local-only values local. Return a bounded safe status.")
	body, err := store.StageRuntimeGoal(context.Background(), goal, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return BoundRuntimeRequest{
		DeviceID:             "ed_11111111111111111111111111111111",
		WorkspaceID:          workspaceID,
		Controller:           ControllerRemoteEdge,
		GoalSummary:          GoalSummary(goal),
		GoalRef:              body.BodyRef,
		GoalDigest:           body.ContentDigest,
		IdempotencyKeyDigest: IdempotencyDigest(key),
		TTL:                  ttl,
	}
}

func TestWorkspaceContinuationCreatesOneActiveRuntimeAndAllowsLaterExplicitRun(t *testing.T) {
	store, err := OpenStore(StoreConfig{Root: filepath.Join(t.TempDir(), "turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspaceID := "ws_22222222222222222222222222222222"
	firstRequest := continuationRequest(t, store, workspaceID, "request-1", time.Minute)
	first, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), firstRequest)
	if err != nil || !created {
		t.Fatalf("first=%+v created=%t err=%v", first, created, err)
	}
	replayed, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), firstRequest)
	if err != nil || created || replayed.RuntimeID != first.RuntimeID {
		t.Fatalf("replayed=%+v created=%t err=%v", replayed, created, err)
	}
	parallelRequest := continuationRequest(t, store, workspaceID, "request-2", time.Minute)
	parallel, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), parallelRequest)
	if err != nil || created || parallel.RuntimeID != first.RuntimeID {
		t.Fatalf("parallel=%+v created=%t err=%v", parallel, created, err)
	}
	if err := store.CompleteRuntime(context.Background(), first.RuntimeID); err != nil {
		t.Fatal(err)
	}
	laterRequest := continuationRequest(t, store, workspaceID, "request-3", time.Minute)
	later, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), laterRequest)
	if err != nil || !created || later.RuntimeID == first.RuntimeID {
		t.Fatalf("later=%+v created=%t err=%v", later, created, err)
	}
}

func TestWorkspaceContinuationNeverRetriesFailedRequestAutomatically(t *testing.T) {
	store, err := OpenStore(StoreConfig{Root: filepath.Join(t.TempDir(), "turns")})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspaceID := "ws_33333333333333333333333333333333"
	request := continuationRequest(t, store, workspaceID, "request-failed", time.Minute)
	runtime, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), request)
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%t err=%v", runtime, created, err)
	}
	if err := store.FailRuntime(context.Background(), runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	replayed, created, err := store.StartWorkspaceContinuationRuntime(context.Background(), request)
	if err != nil || created || replayed.RuntimeID != runtime.RuntimeID || replayed.State != RuntimeStateFailed {
		t.Fatalf("replayed=%+v created=%t err=%v", replayed, created, err)
	}
}
