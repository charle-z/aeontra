package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriverFourTurnsSwitchesFromInlineToReferenceAcrossStoreConnections(t *testing.T) {
	root := filepath.Join(t.TempDir(), "model-turns")
	controller, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	driverStore, err := OpenStore(StoreConfig{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer driverStore.Close()
	runtime, err := controller.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	driver, err := NewDriver(driverStore)
	if err != nil {
		t.Fatal(err)
	}
	handler := driver.Handler()

	for sequence := uint64(1); sequence <= 4; sequence++ {
		statusRequest := httptest.NewRequest(http.MethodGet, "/v1/runtimes/"+runtime.RuntimeID, nil)
		statusRequest.SetPathValue("runtimeID", runtime.RuntimeID)
		statusResponse := httptest.NewRecorder()
		handler.ServeHTTP(statusResponse, statusRequest)
		if statusResponse.Code != http.StatusOK {
			metrics := callDriverRaw(t, handler, http.MethodGet, "/v1/metrics", nil, nil, http.StatusOK)
			t.Fatalf("sequence=%d status=%d body=%s metrics=%v", sequence, statusResponse.Code, statusResponse.Body.String(), metrics)
		}
		payload := []byte(`{"model_id":"external","prompt":[{"content":"turn-` + string(rune('0'+sequence)) + `","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`)
		if sequence == 4 {
			payload = []byte(`{"model_id":"external","prompt":[{"content":"` + strings.Repeat("x", int(MaxInlineRequestBytes)+4096) + `","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`)
		}
		digest := digestBytes(payload)
		create := map[string]any{
			"runtime_id": runtime.RuntimeID, "sequence": sequence, "request_digest": digest,
			"ttl_ms": int64(time.Minute / time.Millisecond),
		}
		if int64(len(payload)) <= MaxInlineRequestBytes {
			create["payload"] = json.RawMessage(payload)
		} else {
			stageRequest := httptest.NewRequest(http.MethodPost, "/v1/request-bodies", bytes.NewReader(payload))
			stageRequest.Header.Set("Content-Type", "application/json")
			stageRequest.Header.Set(requestDigestHeader, digest)
			stageRequest.Header.Set(requestTTLHeader, "60000")
			stageResponse := httptest.NewRecorder()
			handler.ServeHTTP(stageResponse, stageRequest)
			if stageResponse.Code != http.StatusCreated {
				t.Fatalf("sequence=%d stage status=%d body=%s", sequence, stageResponse.Code, stageResponse.Body.String())
			}
			var reference RequestBodyReference
			if err := json.Unmarshal(stageResponse.Body.Bytes(), &reference); err != nil {
				t.Fatal(err)
			}
			create["request_ref"] = reference.RequestRef
		}
		createBody, _ := json.Marshal(create)
		createRequest := httptest.NewRequest(http.MethodPost, "/v1/turns", bytes.NewReader(createBody))
		createRequest.Header.Set("Content-Type", "application/json")
		createResponse := httptest.NewRecorder()
		handler.ServeHTTP(createResponse, createRequest)
		if createResponse.Code != http.StatusCreated {
			metrics := callDriverRaw(t, handler, http.MethodGet, "/v1/metrics", nil, nil, http.StatusOK)
			t.Fatalf("sequence=%d create status=%d body=%s metrics=%v", sequence, createResponse.Code, createResponse.Body.String(), metrics)
		}
		var turn Turn
		if err := json.Unmarshal(createResponse.Body.Bytes(), &turn); err != nil {
			t.Fatal(err)
		}
		offer, err := controller.Next(context.Background(), runtime.RuntimeID)
		if err != nil {
			t.Fatal(err)
		}
		if offer.TurnID != turn.ID || !bytes.Equal(offer.RequestPayload, payload) {
			t.Fatalf("sequence=%d offer=%+v payload_bytes=%d", sequence, offer.Record, len(offer.RequestPayload))
		}
		responsePayload := json.RawMessage(`{"finish_reason":"stop","text":"ok","tool_calls":[],"usage":null}`)
		if _, err := controller.Respond(context.Background(), ResponseSubmission{
			RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: sequence,
			RequestDigest: digest, Payload: responsePayload,
		}); err != nil {
			t.Fatal(err)
		}
		waitRequest := httptest.NewRequest(http.MethodGet, "/v1/turns/"+string(turn.ID)+"/response", nil)
		waitRequest.SetPathValue("turnID", string(turn.ID))
		waitResponse := httptest.NewRecorder()
		handler.ServeHTTP(waitResponse, waitRequest)
		if waitResponse.Code != http.StatusOK {
			t.Fatalf("sequence=%d wait status=%d body=%s", sequence, waitResponse.Code, waitResponse.Body.String())
		}
	}
	stats, err := controller.Stats(context.Background())
	if err != nil || stats.TurnCount != 4 || stats.ConsumedCount != 4 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}
