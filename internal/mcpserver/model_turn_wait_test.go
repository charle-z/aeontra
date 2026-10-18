package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelTurnNextWaitsForNewSequence(t *testing.T) {
	server, store := modelTurnServer(t)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","after_sequence":0,"wait_seconds":2}`), "session-a")
		done <- result{text: text, err: err}
	}()
	waitForModelWaitRegistration(t, server, "session-a", runtimeRecord.RuntimeID)
	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"wait"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !strings.Contains(got.text, `"status":"turn"`) || !strings.Contains(got.text, `"turn_id":"`+string(turn.ID)+`"`) {
			t.Fatalf("text=%s err=%v", got.text, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll did not return the created turn")
	}
}

func TestModelTurnNextRejectsDuplicateWaitForSameSession(t *testing.T) {
	server, store := modelTurnServer(t)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","wait_seconds":2}`), "same-session")
		done <- err
	}()
	waitForModelWaitRegistration(t, server, "same-session", runtimeRecord.RuntimeID)
	if _, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","wait_seconds":1}`), "same-session"); !errors.Is(err, modelturn.ErrTurnConflict) {
		t.Fatalf("duplicate wait err=%v", err)
	}
	if _, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"release"}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("original wait did not finish")
	}
}

func TestModelTurnNextAllowsDifferentSessionsAndTimesOutCompactly(t *testing.T) {
	server, store := modelTurnServer(t)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		text string
		err  error
	}
	results := make(chan result, 2)
	for _, session := range []string{"session-a", "session-b"} {
		session := session
		go func() {
			text, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","wait_seconds":2}`), session)
			results <- result{text: text, err: err}
		}()
	}
	waitForModelWaitRegistration(t, server, "session-a", runtimeRecord.RuntimeID)
	waitForModelWaitRegistration(t, server, "session-b", runtimeRecord.RuntimeID)
	if _, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"broadcast"}`),
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil || !strings.Contains(got.text, `"status":"turn"`) {
				t.Fatalf("text=%s err=%v", got.text, got.err)
			}
		case <-time.After(time.Second):
			t.Fatal("session wait did not finish")
		}
	}

	started := time.Now()
	text, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","after_sequence":1,"wait_seconds":1}`), "timeout-session")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("timeout elapsed=%s", elapsed)
	}
	if !strings.Contains(text, `"status":"no_change"`) || !strings.Contains(text, `"last_sequence":1`) {
		t.Fatalf("timeout text=%s", text)
	}
}

func TestModelTurnNextReturnsDisconnectedAndCancelledStates(t *testing.T) {
	server, store := modelTurnServer(t)
	runtimeRecord, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.CreateTurn(context.Background(), modelturn.ModelRequest{
		RuntimeID: runtimeRecord.RuntimeID,
		Sequence:  1,
		Payload:   json.RawMessage(`{"prompt":"disconnect"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDisconnected(context.Background(), turn.ID); err != nil {
		t.Fatal(err)
	}
	text, err := server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","after_sequence":1,"wait_seconds":1}`), "session-a")
	if err != nil || !strings.Contains(text, `"status":"disconnected"`) {
		t.Fatalf("disconnected text=%s err=%v", text, err)
	}
	if err := store.CancelRuntime(context.Background(), runtimeRecord.RuntimeID); err != nil {
		t.Fatal(err)
	}
	text, err = server.handleModelTurnNext(json.RawMessage(`{"runtime_id":"`+runtimeRecord.RuntimeID+`","after_sequence":1,"wait_seconds":1}`), "session-a")
	if err != nil || !strings.Contains(text, `"status":"cancelled"`) {
		t.Fatalf("cancelled text=%s err=%v", text, err)
	}
}

func waitForModelWaitRegistration(t *testing.T, server *Server, sessionKey, runtimeID string) {
	t.Helper()
	key := sessionKey + "\x00" + runtimeID
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		server.modelWaitMu.Lock()
		_, exists := server.modelWaits[key]
		server.modelWaitMu.Unlock()
		if exists {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("model wait was not registered for %s", sessionKey)
}
