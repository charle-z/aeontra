package modelturn

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeObservabilityNormalizesClockRegressionOnExpiry(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 28, 13, 30, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Second)
	clock.Add(10 * time.Second)
	if err := store.RecordRuntimePhase(context.Background(), runtime.RuntimeID, RuntimePhaseLocalPreflightComplete, "", 1); err != nil {
		t.Fatal(err)
	}
	clock.Add(-8 * time.Second)
	if err := store.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	view, err := store.Runtime(context.Background(), runtime.RuntimeID)
	if err != nil || view.State != RuntimeStateExpired || len(view.Phases) != 3 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	if view.Phases[1].Phase != RuntimePhaseLocalPreflightComplete || view.Phases[2].Phase != RuntimePhaseTerminal {
		t.Fatalf("phases=%+v", view.Phases)
	}
	if view.Phases[2].Timestamp.Before(view.Phases[1].LastTimestamp) || view.Phases[2].DurationMS < 0 {
		t.Fatalf("clock regression was not normalized: %+v", view.Phases)
	}
}
