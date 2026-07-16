package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestConsumeRespondedTurnWaitsForConcurrentWriterWithoutBusySnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-turns")
	controller, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	consumer, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()

	runtime, err := controller.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request := json.RawMessage(`{"model_id":"external","prompt":[],"protocol_version":"mcp-devbox.model-turn.v1"}`)
	turn, err := controller.CreateTurn(context.Background(), ModelRequest{
		RuntimeID: runtime.RuntimeID,
		Sequence:  1,
		Payload:   request,
		TTL:       time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := json.RawMessage(`{"finish_reason":"stop","text":"ok","tool_calls":[],"usage":null}`)
	if _, err := controller.Respond(context.Background(), ResponseSubmission{
		RuntimeID:        runtime.RuntimeID,
		TurnID:           turn.ID,
		ExpectedSequence: 1,
		RequestDigest:    turn.RequestDigest,
		Payload:          response,
	}); err != nil {
		t.Fatal(err)
	}

	writer, err := controller.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(context.Background(), `UPDATE model_runtimes SET updated_at=updated_at WHERE runtime_id=?`, runtime.RuntimeID); err != nil {
		_ = writer.Rollback()
		t.Fatal(err)
	}

	type result struct {
		response ModelResponse
		ready    bool
		err      error
	}
	done := make(chan result, 1)
	go func() {
		got, ready, err := consumer.consumeOnce(context.Background(), turn.ID)
		done <- result{response: got, ready: ready, err: err}
	}()

	select {
	case got := <-done:
		_ = writer.Rollback()
		t.Fatalf("consume returned while a concurrent writer held the database: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if got.err != nil || !got.ready {
			t.Fatalf("consume result=%+v", got)
		}
		if got.response.TurnID != turn.ID || got.response.RequestDigest != turn.RequestDigest || !bytes.Equal(got.response.Payload, response) {
			t.Fatalf("response=%+v", got.response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consume did not resume after the concurrent writer committed")
	}

	stats, err := controller.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.ConsumedCount != 1 || stats.TurnCount != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}
