package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRuntimeEfficiencyMeasuresExistingEvidenceAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)}
	store, root := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	clock.Add(2 * time.Second)
	if _, found, err := store.LeaseNextRuntime(ctx, observabilityDeviceID); err != nil || !found {
		t.Fatalf("lease found=%v err=%v", found, err)
	}
	clock.Add(time.Second)
	if err := store.RecordRuntimePhase(ctx, runtime.RuntimeID, RuntimePhaseLeaseRetry, RuntimeRetryServerBusy, 3); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	request := validRequest(1)
	request.RuntimeID = runtime.RuntimeID
	turn, err := store.CreateTurn(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(5 * time.Second)
	if _, err := store.Respond(ctx, ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: turn.RequestDigest,
		Payload: json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[{"tool_id":"tool-read","arguments":{}}]}`), UsedToolIDs: []string{"tool-read"},
	}); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Second)
	if _, err := store.WaitResponse(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	clock.Add(2 * time.Second)
	if err := store.CompleteRuntime(ctx, runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ElapsedMS == nil || *view.ElapsedMS != 12000 || view.QueueMS == nil || *view.QueueMS != 2000 || view.StartupMS == nil || *view.StartupMS != 2000 || view.TimeToFirstTurnMS == nil || *view.TimeToFirstTurnMS != 4000 {
		t.Fatalf("timings=%+v", view)
	}
	if !view.RetryMeasurementKnown || view.RecordedLeaseRetries != 3 || view.RetryCountLowerBound || view.ObservedTurns != 1 || view.RespondedTurns != 1 || view.ConsumedTurns != 1 || view.ResponseWaitMS != 5000 || view.MeasuredResponseWaitTurns != 1 || view.UnrespondedTerminalTurns != 0 || view.TurnSampleTruncated {
		t.Fatalf("counts=%+v", view)
	}
	encoded, _ := json.Marshal(view)
	for _, forbidden := range []string{"runtime_id", "private prompt", "tool-read", "arguments", "goal", "path", "token", "cost", "digest", "workspace"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("efficiency leaked %q: %s", forbidden, encoded)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	clock.Add(3 * time.Minute)
	reopened, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	restoredJSON, _ := json.Marshal(restored)
	if string(restoredJSON) != string(encoded) {
		t.Fatalf("restart changed completed metrics: before=%s after=%s", encoded, restoredJSON)
	}
}

func TestRuntimeEfficiencyReadIsOptionalAndDoesNotExpireOrWake(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 13, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Second)
	clock.Add(2 * time.Second)
	// Metrics must keep working without body tables and with SQLite writes
	// disabled, even when the runtime would otherwise need expiry cleanup.
	if _, err := store.db.Exec(`DROP TABLE turn_bodies`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE runtime_bodies`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`PRAGMA query_only=ON`); err != nil {
		t.Fatal(err)
	}
	wake := store.waitChannel()
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if view.ElapsedMS == nil || *view.ElapsedMS != 2000 || view.QueueMS != nil || view.StartupMS != nil || view.TimeToFirstTurnMS != nil || view.RetryMeasurementKnown || view.RecordedLeaseRetries != 0 || view.ObservedTurns != 0 {
		t.Fatalf("unknown measurements were fabricated: %+v", view)
	}
	var state string
	if err := store.db.QueryRow(`SELECT state FROM model_runtimes WHERE runtime_id=?`, runtime.RuntimeID).Scan(&state); err != nil || state != string(RuntimeStateAwaitingEdge) {
		t.Fatalf("read mutated expired state=%s err=%v", state, err)
	}
	select {
	case <-wake:
		t.Fatal("metrics read woke workers")
	default:
	}
	for _, id := range []string{"", "../private", "private prompt"} {
		if _, err := store.RuntimeEfficiency(ctx, id); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid ID %q err=%v", id, err)
		}
	}
	if _, err := store.RuntimeEfficiency(ctx, "missing-runtime"); !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("missing runtime err=%v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.RuntimeEfficiency(cancelled, runtime.RuntimeID); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}

func TestRuntimeEfficiencyDoesNotLabelInterruptedTurnsAsDuplicateWork(t *testing.T) {
	for _, terminal := range []RuntimeState{RuntimeStateCancelled, RuntimeStateFailed, RuntimeStateExpired} {
		t.Run(string(terminal), func(t *testing.T) {
			ctx := context.Background()
			clock := &testClock{now: time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)}
			store, _ := openTestStore(t, clock, 0)
			runtime := startObservedRuntime(t, store, time.Second)
			request := validRequest(1)
			request.RuntimeID = runtime.RuntimeID
			request.TTL = time.Second
			if _, err := store.CreateTurn(ctx, request); err != nil {
				t.Fatal(err)
			}
			clock.Add(2 * time.Second)
			switch terminal {
			case RuntimeStateCancelled:
				if err := store.CancelRuntime(ctx, runtime.RuntimeID); err != nil {
					t.Fatal(err)
				}
			case RuntimeStateFailed:
				if err := store.FailRuntime(ctx, runtime.RuntimeID); err != nil {
					t.Fatal(err)
				}
			case RuntimeStateExpired:
				if err := store.Cleanup(ctx); err != nil {
					t.Fatal(err)
				}
			}
			view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
			if err != nil {
				t.Fatal(err)
			}
			if view.ElapsedMS == nil || *view.ElapsedMS != 2000 || view.ObservedTurns != 1 || view.RespondedTurns != 0 || view.ResponseWaitMS != 0 || view.MeasuredResponseWaitTurns != 0 {
				t.Fatalf("terminal metrics=%+v", view)
			}
			wantUnresponded := int64(1)
			if terminal == RuntimeStateFailed {
				// Failure does not rewrite the outstanding turn's lifecycle.
				wantUnresponded = 0
			}
			if view.UnrespondedTerminalTurns != wantUnresponded {
				t.Fatalf("interrupted turns=%d want=%d", view.UnrespondedTerminalTurns, wantUnresponded)
			}
		})
	}
}

func TestRuntimeEfficiencyBoundsTurnSampleAndRetryMeasurements(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	for range 11 {
		if err := store.RecordRuntimePhase(ctx, runtime.RuntimeID, RuntimePhaseLeaseRetry, RuntimeRetryNoContent, 100); err != nil {
			t.Fatal(err)
		}
	}
	// Seed only metadata to exercise the bound without reading/writing bodies.
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for sequence := 1; sequence <= MaxEfficiencyTurnSamples+1; sequence++ {
		if _, err := tx.Exec(`INSERT INTO model_turns(turn_id,runtime_id,sequence,request_digest,request_ref,status,created_at,expires_at,offered_tools_json) VALUES(printf('turn-%d',?),?,?,'',printf('request-%d',?), 'cancelled',?,?,'[]')`, sequence, runtime.RuntimeID, sequence, sequence, clock.Now().UnixNano(), clock.Now().Add(time.Minute).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.TurnSampleTruncated || view.ObservedTurns != MaxEfficiencyTurnSamples || view.UnrespondedTerminalTurns != MaxEfficiencyTurnSamples || !view.RetryMeasurementKnown || !view.RetryCountLowerBound || view.RecordedLeaseRetries != 1000 {
		t.Fatalf("bounded metrics=%+v", view)
	}
}

func TestRuntimeEfficiencyUnknownTerminalTimingAndClockRegression(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	clock.Add(-time.Second)
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.ElapsedMS != nil {
		t.Fatalf("clock regression metrics=%+v err=%v", view, err)
	}
	if _, err := store.db.Exec(`UPDATE model_runtimes SET state='completed',status='completed' WHERE runtime_id=?`, runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err = store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.ElapsedMS != nil {
		t.Fatalf("missing terminal timestamp metrics=%+v err=%v", view, err)
	}
}

func TestRuntimeEfficiencySeparatesMissingResponseTimingFromZeroWait(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 17, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	if _, err := store.db.Exec(`INSERT INTO model_turns(turn_id,runtime_id,sequence,request_digest,request_ref,status,created_at,expires_at,offered_tools_json) VALUES('turn-unknown',?,1,'','request-unknown','consumed',?,?,'[]')`, runtime.RuntimeID, clock.Now().UnixNano(), clock.Now().Add(time.Minute).UnixNano()); err != nil {
		t.Fatal(err)
	}
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.ObservedTurns != 1 || view.RespondedTurns != 1 || view.ConsumedTurns != 1 || view.MeasuredResponseWaitTurns != 0 || view.ResponseWaitMS != 0 {
		t.Fatalf("missing response timing=%+v err=%v", view, err)
	}
	if _, err := store.db.Exec(`UPDATE model_turns SET responded_at=created_at WHERE runtime_id=?`, runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err = store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.MeasuredResponseWaitTurns != 1 || view.ResponseWaitMS != 0 {
		t.Fatalf("known zero response timing=%+v err=%v", view, err)
	}
	if _, err := store.db.Exec(`UPDATE model_turns SET responded_at=created_at-1000000000 WHERE runtime_id=?`, runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err = store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.RespondedTurns != 1 || view.MeasuredResponseWaitTurns != 0 || view.ResponseWaitMS != 0 {
		t.Fatalf("regressed response timing=%+v err=%v", view, err)
	}
}

func TestRuntimeEfficiencyRejectsInvalidMetadata(t *testing.T) {
	for _, test := range []struct {
		name, update string
	}{
		{"unknown-runtime-state", `UPDATE model_runtimes SET state='unknown' WHERE runtime_id=?`},
		{"nonretry-category", `UPDATE runtime_phase_events SET category='server_busy' WHERE runtime_id=? AND phase='runtime_created'`},
		{"nonretry-count", `UPDATE runtime_phase_events SET count=2 WHERE runtime_id=? AND phase='runtime_created'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 10, 7, 18, 0, 0, 0, time.UTC)}
			store, _ := openTestStore(t, clock, 0)
			runtime := startObservedRuntime(t, store, time.Minute)
			if _, err := store.db.Exec(test.update, runtime.RuntimeID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.RuntimeEfficiency(context.Background(), runtime.RuntimeID); err == nil {
				t.Fatal("invalid metadata was presented as an observed efficiency snapshot")
			}
		})
	}
}

func TestRuntimeEfficiencyDoesNotInferMilestoneTimesFromRetryLastTimestamp(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: time.Date(2026, 10, 7, 19, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	runtime := startObservedRuntime(t, store, time.Minute)
	for _, phase := range []struct {
		phase       RuntimePhase
		category    RuntimeRetryCategory
		count       uint32
		first, last time.Duration
	}{
		{RuntimePhaseLeaseRetry, RuntimeRetryServerBusy, 2, time.Second, 9 * time.Second},
		{RuntimePhaseLeaseAssigned, "", 1, 2 * time.Second, 2 * time.Second},
		{RuntimePhaseFirstTurnCreated, "", 1, 4 * time.Second, 4 * time.Second},
		{RuntimePhaseTerminal, "", 1, 5 * time.Second, 5 * time.Second},
	} {
		if _, err := store.db.Exec(`INSERT INTO runtime_phase_events(runtime_id,phase,category,count,occurred_at,last_at) VALUES(?,?,?,?,?,?)`, runtime.RuntimeID, phase.phase, phase.category, phase.count, runtime.CreatedAt.Add(phase.first).UnixNano(), runtime.CreatedAt.Add(phase.last).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`UPDATE model_runtimes SET state='completed',status='completed' WHERE runtime_id=?`, runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err := store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.ElapsedMS == nil || *view.ElapsedMS != 5000 || view.QueueMS == nil || *view.QueueMS != 2000 || view.StartupMS == nil || *view.StartupMS != 2000 || view.TimeToFirstTurnMS == nil || *view.TimeToFirstTurnMS != 4000 || view.RecordedLeaseRetries != 2 {
		t.Fatalf("retry aggregation shifted recorded milestones: %+v err=%v", view, err)
	}
	// A genuinely reversed interval is unknown, not a fabricated zero.
	if _, err := store.db.Exec(`UPDATE runtime_phase_events SET occurred_at=? WHERE runtime_id=? AND phase='first_model_turn_created'`, runtime.CreatedAt.Add(time.Second).UnixNano(), runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err = store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.StartupMS != nil || view.TimeToFirstTurnMS == nil || *view.TimeToFirstTurnMS != 1000 {
		t.Fatalf("reversed milestone interval was fabricated: %+v err=%v", view, err)
	}
	if _, err := store.db.Exec(`UPDATE runtime_phase_events SET occurred_at=? WHERE runtime_id=? AND phase='lease_assigned'`, runtime.CreatedAt.Add(-time.Second).UnixNano(), runtime.RuntimeID); err != nil {
		t.Fatal(err)
	}
	view, err = store.RuntimeEfficiency(ctx, runtime.RuntimeID)
	if err != nil || view.QueueMS != nil || view.StartupMS != nil {
		t.Fatalf("pre-creation milestone was fabricated: %+v err=%v", view, err)
	}
}
