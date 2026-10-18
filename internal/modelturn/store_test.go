package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func openTestStore(t *testing.T, clock *testClock, quota int64) (*Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "model-turns")
	store, err := OpenStore(StoreConfig{Root: root, QuotaBytes: quota, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, root
}

func validRequest(sequence uint64) ModelRequest {
	return ModelRequest{
		RuntimeID: "runtime-1",
		Sequence:  sequence,
		Payload:   json.RawMessage(`{"messages":[{"role":"user","content":"read the repository"}]}`),
		OfferedTools: []ToolDefinition{
			{ID: "tool-read", Name: "read_file", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
			{ID: "tool-test", Name: "run_tests", Schema: json.RawMessage(`{"type":"object","properties":{}}`)},
		},
	}
}

func TestStoreCreateUsesExactMetadataSchemaAndCanonicalDigest(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	request := validRequest(1)
	request.Payload = json.RawMessage(` { "messages" : [ { "content" : "secret-body", "role" : "user" } ] } `)
	turn, err := store.CreateTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if turn.RuntimeID != request.RuntimeID || turn.Sequence != 1 || turn.ID == "" || turn.RequestDigest == "" || !turn.ExpiresAt.Equal(clock.now.Add(DefaultTurnTTL)) {
		t.Fatalf("turn=%+v", turn)
	}
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusAwaitingModel || record.RequestRef == "" || record.ResponseRef != "" || record.ResponseDigest != "" {
		t.Fatalf("record=%+v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-body") || strings.Contains(string(encoded), "messages") {
		t.Fatalf("metadata leaked request body: %s", encoded)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"runtime_id", "turn_id", "sequence", "request_digest", "request_ref", "response_digest", "response_ref", "status", "created_at", "expires_at", "responded_at", "consumed_at"}
	if len(fields) != len(wantKeys) {
		t.Fatalf("record schema=%v", fields)
	}
	for _, key := range wantKeys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s in %s", key, encoded)
		}
	}
	offer, err := store.nextOnce(context.Background(), request.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(offer.RequestPayload, []byte(`{"messages":[{"content":"secret-body","role":"user"}]}`)) {
		t.Fatalf("canonical payload=%s", offer.RequestPayload)
	}
	if !reflect.DeepEqual(offer.OfferedToolIDs, []string{"tool-read", "tool-test"}) {
		t.Fatalf("offered=%v", offer.OfferedToolIDs)
	}
}

func TestStoreRespondConsumeAndRejectReplay(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	turn, err := store.CreateTurn(context.Background(), validRequest(1))
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[{"tool_id":"tool-read","arguments":{"path":"README.md"}}]}`)
	submission := ResponseSubmission{
		RuntimeID:        turn.RuntimeID,
		TurnID:           turn.ID,
		ExpectedSequence: turn.Sequence,
		RequestDigest:    turn.RequestDigest,
		Payload:          payload,
		UsedToolIDs:      []string{"tool-read"},
	}
	record, err := store.Respond(context.Background(), submission)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != StatusResponded || record.ResponseDigest == "" || record.ResponseRef == "" || record.RespondedAt == nil {
		t.Fatalf("responded record=%+v", record)
	}
	if _, err := store.Respond(context.Background(), submission); !errors.Is(err, ErrResponseReplay) {
		t.Fatalf("duplicate response error=%v", err)
	}
	response, err := store.WaitResponse(context.Background(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantPayload, err := canonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if response.RuntimeID != turn.RuntimeID || response.TurnID != turn.ID || response.Sequence != turn.Sequence || response.RequestDigest != turn.RequestDigest || !bytes.Equal(response.Payload, wantPayload) {
		t.Fatalf("response=%+v", response)
	}
	if _, err := store.WaitResponse(context.Background(), turn.ID); !errors.Is(err, ErrResponseReplay) {
		t.Fatalf("second consume error=%v", err)
	}
	consumed, err := store.Get(context.Background(), turn.ID)
	if err != nil || consumed.Status != StatusConsumed || consumed.ConsumedAt == nil {
		t.Fatalf("consumed=%+v err=%v", consumed, err)
	}
}

func TestStoreSequenceCASAndInventedToolsFailClosed(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	if _, err := store.CreateTurn(context.Background(), validRequest(2)); !errors.Is(err, ErrSequenceMismatch) {
		t.Fatalf("initial gap error=%v", err)
	}
	turn, err := store.CreateTurn(context.Background(), validRequest(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTurn(context.Background(), validRequest(1)); !errors.Is(err, ErrSequenceMismatch) {
		t.Fatalf("duplicate sequence error=%v", err)
	}
	base := ResponseSubmission{RuntimeID: turn.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"finish_reason":"tool_calls"}`)}
	wrongSequence := base
	wrongSequence.ExpectedSequence++
	if _, err := store.Respond(context.Background(), wrongSequence); !errors.Is(err, ErrSequenceMismatch) {
		t.Fatalf("wrong sequence error=%v", err)
	}
	wrongDigest := base
	wrongDigest.RequestDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := store.Respond(context.Background(), wrongDigest); !errors.Is(err, ErrSequenceMismatch) {
		t.Fatalf("wrong digest error=%v", err)
	}
	invented := base
	invented.UsedToolIDs = []string{"tool-invented"}
	if _, err := store.Respond(context.Background(), invented); !errors.Is(err, ErrToolNotOffered) {
		t.Fatalf("invented tool error=%v", err)
	}
	duplicate := base
	duplicate.UsedToolIDs = []string{"tool-read", "tool-read"}
	if _, err := store.Respond(context.Background(), duplicate); err != nil {
		t.Fatalf("repeated offered tool error=%v", err)
	}
}

func TestStoreRejectsLateResponseAndSupportsCancellation(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	request := validRequest(1)
	request.TTL = time.Second
	turn, err := store.CreateTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	clock.Add(2 * time.Second)
	_, err = store.Respond(context.Background(), ResponseSubmission{RuntimeID: turn.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"finish_reason":"stop"}`)})
	if !errors.Is(err, ErrLateResponse) {
		t.Fatalf("late response error=%v", err)
	}
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil || record.Status != StatusExpired {
		t.Fatalf("expired record=%+v err=%v", record, err)
	}

	clock.Add(time.Second)
	second, err := store.CreateTurn(context.Background(), validRequest(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(context.Background(), second.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.Get(context.Background(), second.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	if err := store.Cancel(context.Background(), second.ID); !errors.Is(err, ErrTurnConflict) {
		t.Fatalf("cancel replay error=%v", err)
	}
}

func TestStoreRestartResumePreservesExactAwaitingTurn(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 13, 0, 0, 0, time.UTC)}
	root := filepath.Join(t.TempDir(), "model-turns")
	first, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := first.CreateTurn(context.Background(), validRequest(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.MarkDisconnected(context.Background(), turn.ID); err != nil {
		t.Fatal(err)
	}
	before, err := first.Get(context.Background(), turn.ID)
	if err != nil || before.Status != StatusDisconnected {
		t.Fatalf("before restart=%+v err=%v", before, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := OpenStore(StoreConfig{Root: root, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	resumed, err := second.ResumeRuntime(context.Background(), turn.RuntimeID)
	if err != nil || resumed != 1 {
		t.Fatalf("resumed=%d err=%v", resumed, err)
	}
	offer, err := second.nextOnce(context.Background(), turn.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if offer.TurnID != turn.ID || offer.Sequence != turn.Sequence || offer.RequestDigest != turn.RequestDigest || offer.RequestRef != before.RequestRef {
		t.Fatalf("resumed offer=%+v original=%+v", offer, turn)
	}
	if _, err := second.Respond(context.Background(), ResponseSubmission{RuntimeID: turn.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"finish_reason":"stop","text":"done"}`)}); err != nil {
		t.Fatal(err)
	}
	response, err := second.WaitResponse(context.Background(), turn.ID)
	if err != nil || !strings.Contains(string(response.Payload), `"text":"done"`) {
		t.Fatalf("response=%s err=%v", response.Payload, err)
	}
}

func TestStoreConcurrentRespondAllowsExactlyOneWinner(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC)}
	store, _ := openTestStore(t, clock, 0)
	turn, err := store.CreateTurn(context.Background(), validRequest(1))
	if err != nil {
		t.Fatal(err)
	}
	submission := ResponseSubmission{RuntimeID: turn.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"finish_reason":"stop"}`)}
	var wg sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Respond(context.Background(), submission)
			errorsFound <- err
		}()
	}
	wg.Wait()
	close(errorsFound)
	wins := 0
	for err := range errorsFound {
		if err == nil {
			wins++
			continue
		}
		if !errors.Is(err, ErrResponseReplay) {
			t.Fatalf("unexpected concurrent error=%v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("successful responses=%d want=1", wins)
	}
}

func TestStoreQuotaBoundsActiveBodiesAndKeepsLargeBodiesOutOfMetadata(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 7, 15, 15, 0, 0, 0, time.UTC)}
	store, root := openTestStore(t, clock, 2048)
	request := validRequest(1)
	request.Payload = json.RawMessage(`{"text":"` + strings.Repeat("x", 1400) + `"}`)
	turn, err := store.CreateTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Get(context.Background(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(record)
	if len(encoded) > 1024 || strings.Contains(string(encoded), strings.Repeat("x", 32)) {
		t.Fatalf("large body entered metadata: bytes=%d", len(encoded))
	}
	_, err = store.Respond(context.Background(), ResponseSubmission{RuntimeID: turn.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence, RequestDigest: turn.RequestDigest, Payload: json.RawMessage(`{"text":"` + strings.Repeat("y", 900) + `"}`)})
	if !errors.Is(err, ErrTurnQuotaExceeded) {
		t.Fatalf("quota error=%v", err)
	}
	info, err := os.Stat(filepath.Join(root, turnDatabaseFilename))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("database mode=%v err=%v", info, err)
	}
}

func TestOpenStoreRejectsUnsafeRoots(t *testing.T) {
	clock := &testClock{now: time.Now().UTC()}
	relative := filepath.Join("relative", "model-turns")
	if _, err := OpenStore(StoreConfig{Root: relative, Now: clock.Now}); err == nil {
		t.Fatal("relative root accepted")
	}
	parent := t.TempDir()
	unsafe := filepath.Join(parent, "unsafe")
	if err := os.Mkdir(unsafe, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(StoreConfig{Root: unsafe, Now: clock.Now}); err == nil {
		t.Fatal("world-readable root accepted")
	}
}
