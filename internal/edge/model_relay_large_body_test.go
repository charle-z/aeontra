package edge

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRelayStagesLargeRequestsOnlyInAuthoritativeStore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-large-body")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "large-body")
	handler := NewHTTPHandler(devices, turns)

	marker := "private-large-prompt-marker"
	payloadBytes, err := json.Marshal(map[string]any{
		"messages": []any{map[string]any{"content": marker + strings.Repeat("x", int(modelturn.MaxInlineRequestBytes)+4096), "role": "user"}},
		"tools":    []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(payloadBytes)
	if int64(len(payload)) <= modelturn.MaxInlineRequestBytes {
		t.Fatalf("fixture is not large: %d", len(payload))
	}
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-large-create", modelRuntimePrefix+runtime.RuntimeID+"/turns", modelTurnCreateRequest{
		CreateID: "ec_44444444444444444444444444444444", Sequence: 1,
		RequestDigest: digest, Payload: payload, TTLMillis: int64((2 * time.Minute) / time.Millisecond),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var turn modelturn.Turn
	if err := json.Unmarshal(response.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(turn.RequestRef, "mb_") || turn.RequestDigest != digest {
		t.Fatalf("turn=%+v", turn)
	}
	record, err := turns.Get(t.Context(), turn.ID)
	if err != nil || record.RequestRef != turn.RequestRef || record.RequestDigest != digest {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	var edgeMetadata string
	if err := devices.db.QueryRow(`SELECT device_id||runtime_id||create_id||turn_id||request_digest||request_ref FROM edge_model_turn_creates WHERE turn_id=?`, turn.ID).Scan(&edgeMetadata); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(edgeMetadata, marker) || strings.Contains(edgeMetadata, strings.Repeat("x", 64)) {
		t.Fatal("Edge idempotency receipt stored request content")
	}
}
