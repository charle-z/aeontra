package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	observabilityDeviceID    = "ed_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	observabilityWorkspaceID = "ws_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func startObservedRuntime(t *testing.T, store *Store, ttl time.Duration) Runtime {
	t.Helper()
	goal := []byte("private prompt body that must never enter observability")
	body, err := store.StageRuntimeGoal(context.Background(), goal, ttl)
	if err != nil {
		t.Fatal(err)
	}
	runtime, created, err := store.StartBoundRuntime(context.Background(), BoundRuntimeRequest{
		DeviceID: observabilityDeviceID, WorkspaceID: observabilityWorkspaceID,
		Controller: ControllerRemoteEdge, GoalSummary: GoalSummary(goal), GoalRef: body.BodyRef,
		GoalDigest: body.ContentDigest, IdempotencyKeyDigest: IdempotencyDigest("observability-test"), TTL: ttl,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%v err=%v", runtime, created, err)
	}
	return runtime
}

func TestRuntimeObservabilityLifecycleIsOrderedDurableAndPrivate(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)}
	root := filepath.Join(t.TempDir(), "model-turns")
	store, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	runtime := startObservedRuntime(t, store, 10*time.Minute)
	clock.Add(time.Second)
	leased, found, err := store.LeaseNextRuntime(context.Background(), observabilityDeviceID)
	if err != nil || !found || leased.RuntimeID != runtime.RuntimeID {
		t.Fatalf("leased=%+v found=%v err=%v", leased, found, err)
	}
	clock.Add(2 * time.Second)
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseLocalPreflightComplete, "", 1); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	if err := store.SetRuntimeStateAndRecordPhase(context.Background(), runtime.RuntimeID, RuntimeStateAwaitingModel, RuntimePhaseStartedConfirmed, RuntimeStateStarting); err != nil {
		t.Fatal(err)
	}
	clock.Add(3 * time.Second)
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseDriverSocketReady, "", 1); err != nil {
		t.Fatal(err)
	}
	clock.Add(4 * time.Second)
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseOpenCodeProcessStarted, "", 1); err != nil {
		t.Fatal(err)
	}
	clock.Add(5 * time.Second)
	request := validRequest(1)
	request.RuntimeID = runtime.RuntimeID
	request.RequestDigest = ""
	turn, err := store.CreateTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(6 * time.Second)
	if _, err := store.Respond(context.Background(), ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: turn.RequestDigest,
		Payload: json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[{"tool_id":"tool-read","arguments":{}}]}`), UsedToolIDs: []string{"tool-read"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WaitResponse(context.Background(), turn.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRuntimeStateAndRecordPhase(context.Background(), runtime.RuntimeID, RuntimeStateExecutingTools, RuntimePhaseToolExecutionStarted, RuntimeStateAwaitingModel); err != nil {
		t.Fatal(err)
	}
	clock.Add(7 * time.Second)
	if err := store.CompleteRuntime(context.Background(), runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}

	view, err := store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	want := []RuntimePhase{RuntimePhaseCreated, RuntimePhaseLeaseAssigned, RuntimePhaseLocalPreflightComplete, RuntimePhaseStartedConfirmed, RuntimePhaseDriverSocketReady, RuntimePhaseOpenCodeProcessStarted, RuntimePhaseFirstTurnCreated, RuntimePhaseToolExecutionStarted, RuntimePhaseTerminal}
	if len(view.Phases) != len(want) {
		t.Fatalf("phases=%+v", view.Phases)
	}
	previous := view.CreatedAt
	for index, phase := range view.Phases {
		if phase.Phase != want[index] || phase.Timestamp.Before(previous) || phase.DurationMS < 0 || phase.SinceCreatedMS < 0 {
			t.Fatalf("index=%d phase=%+v previous=%s", index, phase, previous)
		}
		previous = phase.LastTimestamp
	}
	encoded, err := json.Marshal(view.Phases)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private prompt", "messages", "tool-read", "arguments", "workspace/", "credential"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("observability leaked %q: %s", forbidden, encoded)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil || len(restored.Phases) != len(want) || restored.Phases[len(restored.Phases)-1].Phase != RuntimePhaseTerminal {
		t.Fatalf("restored=%+v err=%v", restored.Phases, err)
	}
}

func TestRuntimeObservabilityTerminalPaths(t *testing.T) {
	t.Run("expires-before-pickup", func(t *testing.T) {
		clock := &testClock{now: time.Date(2026, 7, 28, 13, 0, 0, 0, time.UTC)}
		store, _ := openTestStore(t, clock, 0)
		runtime := startObservedRuntime(t, store, time.Second)
		clock.Add(2 * time.Second)
		if _, found, err := store.LeaseNextRuntime(context.Background(), observabilityDeviceID); err != nil || found {
			t.Fatalf("expired runtime lease found=%v err=%v", found, err)
		}
		view, err := store.Runtime(context.Background(), runtime.RuntimeID)
		if err != nil || view.State != RuntimeStateExpired || view.Phases[len(view.Phases)-1].Phase != RuntimePhaseTerminal {
			t.Fatalf("view=%+v err=%v", view, err)
		}
	})
	t.Run("startup-failure", func(t *testing.T) {
		clock := &testClock{now: time.Date(2026, 7, 28, 14, 0, 0, 0, time.UTC)}
		store, _ := openTestStore(t, clock, 0)
		runtime := startObservedRuntime(t, store, time.Minute)
		if _, found, err := store.LeaseNextRuntime(context.Background(), observabilityDeviceID); err != nil || !found {
			t.Fatalf("found=%v err=%v", found, err)
		}
		if err := store.FailRuntime(context.Background(), runtime.RuntimeID); err != nil {
			t.Fatal(err)
		}
		view, _ := store.Runtime(context.Background(), runtime.RuntimeID)
		if view.State != RuntimeStateFailed || view.Phases[len(view.Phases)-1].Phase != RuntimePhaseTerminal {
			t.Fatalf("view=%+v", view)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		clock := &testClock{now: time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC)}
		store, _ := openTestStore(t, clock, 0)
		runtime := startObservedRuntime(t, store, time.Minute)
		if err := store.CancelRuntime(context.Background(), runtime.RuntimeID); err != nil {
			t.Fatal(err)
		}
		view, _ := store.Runtime(context.Background(), runtime.RuntimeID)
		if view.State != RuntimeStateCancelled || view.Phases[len(view.Phases)-1].Phase != RuntimePhaseTerminal {
			t.Fatalf("view=%+v", view)
		}
	})
}

func TestRuntimeObservabilityRetryAggregationAndConcurrency(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseLeaseRetry, RuntimeRetryTransportError, 1)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	view, err := store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Phases) > MaxRuntimePhaseEvents {
		t.Fatalf("phase count=%d", len(view.Phases))
	}
	var retry RuntimePhaseEvent
	for _, phase := range view.Phases {
		if phase.Phase == RuntimePhaseLeaseRetry {
			retry = phase
		}
	}
	if retry.RetryCategory != RuntimeRetryTransportError || retry.Count != 32 {
		t.Fatalf("retry=%+v", retry)
	}
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseLeaseRetry, "private-body", 1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid category error=%v", err)
	}
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseDriverSocketReady, "", 0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid count error=%v", err)
	}
	if err := store.SetRuntimeStateAndRecordPhase(context.Background(), runtime.RuntimeID, RuntimeStateAwaitingModel, RuntimePhaseLeaseRetry, RuntimeStateAwaitingEdge); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid atomic phase error=%v", err)
	}
	unchanged, err := store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil || unchanged.State != RuntimeStateAwaitingEdge {
		t.Fatalf("invalid phase changed runtime: state=%s err=%v", unchanged.State, err)
	}
}

func TestRuntimeAcceptsEdgePhaseRequiresAuthoritativeOrder(t *testing.T) {
	runtime := Runtime{State: RuntimeStateStarting, Phases: []RuntimePhaseEvent{{Phase: RuntimePhaseCreated}}}
	if RuntimeAcceptsEdgePhase(runtime, RuntimePhaseLocalPreflightComplete) {
		t.Fatal("preflight accepted before lease assignment")
	}
	runtime.Phases = append(runtime.Phases, RuntimePhaseEvent{Phase: RuntimePhaseLeaseAssigned})
	if !RuntimeAcceptsEdgePhase(runtime, RuntimePhaseLocalPreflightComplete) || RuntimeAcceptsEdgePhase(runtime, RuntimePhaseDriverSocketReady) {
		t.Fatalf("starting phase order was not enforced: %+v", runtime.Phases)
	}
	runtime.State = RuntimeStateAwaitingModel
	runtime.Phases = append(runtime.Phases, RuntimePhaseEvent{Phase: RuntimePhaseLocalPreflightComplete}, RuntimePhaseEvent{Phase: RuntimePhaseStartedConfirmed})
	if !RuntimeAcceptsEdgePhase(runtime, RuntimePhaseDriverSocketReady) || RuntimeAcceptsEdgePhase(runtime, RuntimePhaseOpenCodeProcessStarted) {
		t.Fatalf("driver phase order was not enforced: %+v", runtime.Phases)
	}
	runtime.Phases = append(runtime.Phases, RuntimePhaseEvent{Phase: RuntimePhaseDriverSocketReady})
	if !RuntimeAcceptsEdgePhase(runtime, RuntimePhaseOpenCodeProcessStarted) {
		t.Fatal("OpenCode process phase rejected after driver readiness")
	}
	runtime.State = RuntimeStateCompleted
	if RuntimeAcceptsEdgePhase(runtime, RuntimePhaseOpenCodeProcessStarted) {
		t.Fatal("terminal runtime accepted an Edge phase")
	}
}
