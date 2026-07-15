package modelturn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRestartedDriverCreatesReferencedFollowupTurn(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-turns")
	controller, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	driver, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}

	runtime, err := controller.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstBody := json.RawMessage(`{"content":"` + strings.Repeat("a", int(MaxInlineRequestBytes)+4096) + `"}`)
	firstRef, err := driver.StageRequestBody(context.Background(), firstBody, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	first, err := driver.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID:     runtime.RuntimeID,
		Sequence:      1,
		RequestRef:    firstRef.RequestRef,
		RequestDigest: firstRef.RequestDigest,
		TTL:           time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.MarkDisconnected(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if resumed, err := controller.ResumeRuntime(context.Background(), runtime.RuntimeID); err != nil || resumed != 1 {
		t.Fatalf("resume rows=%d err=%v", resumed, err)
	}
	if _, err := controller.Respond(context.Background(), ResponseSubmission{
		RuntimeID:        runtime.RuntimeID,
		TurnID:           first.ID,
		ExpectedSequence: 1,
		RequestDigest:    first.RequestDigest,
		Payload:          json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[]}`),
	}); err != nil {
		t.Fatal(err)
	}

	restarted, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	response, err := restarted.WaitResponse(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response.TurnID != first.ID || response.Sequence != first.Sequence || response.RequestDigest != first.RequestDigest {
		t.Fatalf("response identity changed: %+v", response)
	}

	secondBody := json.RawMessage(`{"content":"` + strings.Repeat("b", int(MaxInlineRequestBytes)+8192) + `"}`)
	secondRef, err := restarted.StageRequestBody(context.Background(), secondBody, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := restarted.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID:     runtime.RuntimeID,
		Sequence:      2,
		RequestRef:    secondRef.RequestRef,
		RequestDigest: secondRef.RequestDigest,
		TTL:           time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := restarted.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 || record.RequestRef != secondRef.RequestRef || second.RequestDigest != secondRef.RequestDigest {
		t.Fatalf("follow-up identity mismatch: turn=%+v record=%+v ref=%+v", second, record, secondRef)
	}
}
