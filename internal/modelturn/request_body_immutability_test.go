package modelturn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestReferencedRequestBodyCannotChangeAfterTurnCreation(t *testing.T) {
	store := openDriverStore(t, 0)
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := json.RawMessage(`{"content":"` + strings.Repeat("x", int(MaxInlineRequestBytes)+1) + `"}`)
	reference, err := store.StageRequestBody(context.Background(), body, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID:     runtime.RuntimeID,
		Sequence:      1,
		RequestRef:    reference.RequestRef,
		RequestDigest: reference.RequestDigest,
		TTL:           time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if turn.RequestDigest != reference.RequestDigest {
		t.Fatalf("turn digest=%s want=%s", turn.RequestDigest, reference.RequestDigest)
	}
	if _, err := store.db.Exec(
		`UPDATE turn_bodies SET content=? WHERE body_ref=?`,
		[]byte(`{"changed":true}`),
		reference.RequestRef,
	); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("body mutation after turn creation was not rejected: %v", err)
	}
	offer, err := store.nextOnce(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if string(offer.RequestPayload) != string(body) || offer.RequestDigest != reference.RequestDigest {
		t.Fatalf("immutable body changed: digest=%s body_bytes=%d", offer.RequestDigest, len(offer.RequestPayload))
	}
}
