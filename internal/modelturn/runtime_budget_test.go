package modelturn

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRemoteRuntimeExecutionBudgetStartsAtFirstTurn(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	goal := []byte("preserve the requested execution budget")
	body, err := store.StageRuntimeGoal(context.Background(), goal, RemoteRuntimeStartupTTL)
	if err != nil {
		t.Fatal(err)
	}
	const executionTTL = 240 * time.Second
	runtime, created, err := store.StartBoundRuntime(context.Background(), BoundRuntimeRequest{
		DeviceID: observabilityDeviceID, WorkspaceID: observabilityWorkspaceID,
		Controller: ControllerRemoteEdge, GoalSummary: GoalSummary(goal), GoalRef: body.BodyRef,
		GoalDigest: body.ContentDigest, IdempotencyKeyDigest: IdempotencyDigest("runtime-budget-test"),
		TTL: RemoteRuntimeStartupTTL, ExecutionTTL: executionTTL,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%v err=%v", runtime, created, err)
	}
	clock.Add(3 * time.Minute)
	if _, found, err := store.LeaseNextRuntime(context.Background(), observabilityDeviceID); err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	firstStarted := clock.Now()
	firstRequest := validRequest(1)
	firstRequest.RuntimeID = runtime.RuntimeID
	firstRequest.RequestDigest = ""
	first, err := store.CreateTurn(context.Background(), firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	wantDeadline := firstStarted.Add(executionTTL)
	if !first.ExpiresAt.Equal(wantDeadline) {
		t.Fatalf("first expiry=%s want=%s", first.ExpiresAt, wantDeadline)
	}
	view, err := store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ExecutionTimeoutSeconds != int(executionTTL/time.Second) || !view.ExpiresAt.Equal(wantDeadline) {
		t.Fatalf("runtime=%+v want deadline=%s", view, wantDeadline)
	}
	clock.Add(30 * time.Second)
	secondRequest := validRequest(2)
	secondRequest.RuntimeID = runtime.RuntimeID
	secondRequest.RequestDigest = ""
	second, err := store.CreateTurn(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !second.ExpiresAt.Equal(wantDeadline) {
		t.Fatalf("second turn renewed budget: expiry=%s want=%s", second.ExpiresAt, wantDeadline)
	}
	view, err = store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil || !view.ExpiresAt.Equal(wantDeadline) {
		t.Fatalf("runtime deadline changed: runtime=%+v err=%v", view, err)
	}
	clock.Add(executionTTL)
	expired, err := store.HeartbeatRuntime(context.Background(), runtime.RuntimeID, observabilityDeviceID)
	if err != nil || expired.State != RuntimeStateExpired {
		t.Fatalf("expired heartbeat runtime=%+v err=%v", expired, err)
	}
	thirdRequest := validRequest(3)
	thirdRequest.RuntimeID = runtime.RuntimeID
	thirdRequest.RequestDigest = ""
	if _, err := store.CreateTurn(context.Background(), thirdRequest); !errors.Is(err, ErrTurnConflict) && !errors.Is(err, ErrLateResponse) {
		t.Fatalf("expired execution budget accepted: %v", err)
	}
}
