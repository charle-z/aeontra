package modelturn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestResponseForAnotherRuntimeIsRejected(t *testing.T) {
	store := openDriverStore(t, 0)
	first, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: first.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Respond(context.Background(), ResponseSubmission{
		RuntimeID:        second.RuntimeID,
		TurnID:           turn.ID,
		ExpectedSequence: turn.Sequence,
		RequestDigest:    turn.RequestDigest,
		Payload:          json.RawMessage(`{"finish_reason":"stop"}`),
	})
	if !errors.Is(err, ErrSequenceMismatch) {
		t.Fatalf("response for another runtime error=%v", err)
	}
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil || record.Status != StatusAwaitingModel {
		t.Fatalf("turn changed after rejected response: record=%+v err=%v", record, err)
	}
}
