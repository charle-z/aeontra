package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

const controllerA = "mc_11111111111111111111111111111111"
const controllerB = "mc_22222222222222222222222222222222"
const controllerC = "mc_33333333333333333333333333333333"

func controlFixture(t *testing.T) (*Store, string, *testClock, RuntimeControlRequest, ResponseSubmission) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, 10, 7, 16, 0, 0, 0, time.UTC)}
	s, root := openTestStore(t, clock, 0)
	runtime, err := s.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := validRequest(1)
	r.RuntimeID = runtime.RuntimeID
	turn, err := s.CreateTurn(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	claim := RuntimeControlRequest{RuntimeID: runtime.RuntimeID, Action: "claim", ControllerID: controllerA, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: turn.RequestDigest}
	sub := ResponseSubmission{RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"finish_reason":"stop"}`)}
	return s, root, clock, claim, sub
}

func TestRuntimeControlHandoffRestartAndLateOwner(t *testing.T) {
	ctx := context.Background()
	s, root, clock, r, sub := controlFixture(t)
	if _, err := s.ControlRuntime(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Action, r.Generation, r.SuccessorID = "prepare", 1, controllerB
	if c, err := s.ControlRuntime(ctx, r); err != nil || c.Phase != "prepared" {
		t.Fatalf("prepare=%+v err=%v", c, err)
	}
	sub.ControllerID, sub.ControlGeneration = controllerA, 1
	if _, err := s.Respond(ctx, sub); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("response during prepare=%v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	state, err := s.RuntimeControl(ctx, r.RuntimeID)
	if err != nil || state.Phase != "prepared" || state.SuccessorID != controllerB {
		t.Fatalf("recover=%+v err=%v", state, err)
	}
	r.Action, r.ControllerID, r.SuccessorID = "ack", controllerC, ""
	if _, err := s.ControlRuntime(ctx, r); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("wrong successor=%v", err)
	}
	r.ControllerID = controllerB
	if _, err := s.ControlRuntime(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Action = "transfer"
	c, err := s.ControlRuntime(ctx, r)
	if err != nil || c.Generation != 2 || c.ControllerID != controllerB || !c.ReleasePending {
		t.Fatalf("transfer=%+v err=%v", c, err)
	}
	if _, err := s.ControlRuntime(ctx, r); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("duplicate transfer=%v", err)
	}
	if _, err := s.Respond(ctx, sub); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("late owner=%v", err)
	}
	prepare := r
	prepare.Action, prepare.Generation, prepare.SuccessorID = "prepare", 2, controllerC
	if _, err := s.ControlRuntime(ctx, prepare); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("unreleased predecessor lost=%v", err)
	}
	r.Action, r.ControllerID = "release", controllerA
	if c, err := s.ControlRuntime(ctx, r); err != nil || c.ReleasePending {
		t.Fatalf("release=%+v err=%v", c, err)
	}
	if _, err := s.ControlRuntime(ctx, r); err != nil {
		t.Fatalf("release retry=%v", err)
	}
	sub.ControllerID, sub.ControlGeneration = controllerB, 2
	if _, err := s.Respond(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WaitResponse(ctx, sub.TurnID); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeControlAbortAndBoundedExactCheckpoint(t *testing.T) {
	ctx := context.Background()
	s, _, _, r, sub := controlFixture(t)
	if _, err := s.ControlRuntime(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Action, r.Generation, r.SuccessorID = "prepare", 1, controllerB
	if _, err := s.ControlRuntime(ctx, r); err != nil {
		t.Fatal(err)
	}
	bad := r
	bad.Action, bad.ControllerID, bad.SuccessorID, bad.ExpectedSequence = "ack", controllerB, "", 2
	if _, err := s.ControlRuntime(ctx, bad); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("stale ACK=%v", err)
	}
	r.Action, r.SuccessorID = "abort", ""
	if c, err := s.ControlRuntime(ctx, r); err != nil || c.Generation != 2 || c.Phase != "owned" {
		t.Fatalf("abort=%+v err=%v", c, err)
	}
	sub.ControllerID, sub.ControlGeneration = controllerA, 1
	if _, err := s.Respond(ctx, sub); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("old generation after abort=%v", err)
	}
	sub.ControlGeneration = 2
	if _, err := s.Respond(ctx, sub); err != nil {
		t.Fatal(err)
	}
	r.Action, r.Generation = "prepare", 2
	r.SuccessorID = controllerB
	if _, err := s.ControlRuntime(ctx, r); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("responded tool batch transferred=%v", err)
	}
}

func TestRuntimeControlTerminalDeadlineAndOldBinaryFence(t *testing.T) {
	for _, terminal := range []string{"failed", "completed", "expired"} {
		t.Run(terminal, func(t *testing.T) {
			ctx := context.Background()
			s, _, clock, r, sub := controlFixture(t)
			if _, err := s.ControlRuntime(ctx, r); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE model_turns SET response_ref='old-binary' WHERE turn_id=?`, r.TurnID); err == nil {
				t.Fatal("old binary bypassed SQL fence")
			}
			if terminal == "expired" {
				clock.Add(MaxTurnTTL + time.Second)
			} else if _, err := s.db.Exec(`UPDATE model_runtimes SET state=?,status=? WHERE runtime_id=?`, terminal, terminal, r.RuntimeID); err != nil {
				t.Fatal(err)
			}
			r.Action, r.Generation, r.SuccessorID = "prepare", 1, controllerB
			if _, err := s.ControlRuntime(ctx, r); !errors.Is(err, ErrRuntimeControlConflict) {
				t.Fatalf("terminal handoff=%v", err)
			}
			sub.ControllerID, sub.ControlGeneration = controllerA, 1
			if _, err := s.Respond(ctx, sub); err == nil {
				t.Fatal("terminal runtime accepted controlled response")
			}
		})
	}
}

func TestRuntimeControlTwoClaimantsHaveOneWinnerAndLegacyUnaffected(t *testing.T) {
	ctx := context.Background()
	s, _, _, r, sub := controlFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, id := range []string{controllerA, controllerB} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			request := r
			request.ControllerID = id
			if _, err := s.ControlRuntime(ctx, request); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("claim winners=%d", wins)
	}
	if _, err := s.Respond(ctx, sub); !errors.Is(err, ErrRuntimeControlConflict) {
		t.Fatalf("unfenced response=%v", err)
	}
	legacy, _, _, _, legacySub := controlFixture(t)
	if _, err := legacy.Respond(ctx, legacySub); err != nil {
		t.Fatalf("legacy response changed=%v", err)
	}
}
