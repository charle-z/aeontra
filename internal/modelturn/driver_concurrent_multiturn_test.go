package modelturn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriverFourTurnsWithConcurrentControllerWaits(t *testing.T) {
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
		sequence := sequence
		payload := []byte(fmt.Sprintf(`{"model_id":"external","prompt":[{"content":"turn-%d","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`, sequence))
		if sequence == 4 {
			payload = []byte(`{"model_id":"external","prompt":[{"content":"` + strings.Repeat("x", int(MaxInlineRequestBytes)+4096) + `","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`)
		}
		digest := digestBytes(payload)
		providerDone := make(chan error, 1)
		var created Turn
		var received ModelResponse
		go func() {
			statusRequest := httptest.NewRequest(http.MethodGet, "/v1/runtimes/"+runtime.RuntimeID, nil)
			statusRequest.SetPathValue("runtimeID", runtime.RuntimeID)
			statusResponse := httptest.NewRecorder()
			handler.ServeHTTP(statusResponse, statusRequest)
			if statusResponse.Code != http.StatusOK {
				providerDone <- fmt.Errorf("status=%d body=%s", statusResponse.Code, statusResponse.Body.String())
				return
			}

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
					providerDone <- fmt.Errorf("stage=%d body=%s", stageResponse.Code, stageResponse.Body.String())
					return
				}
				var reference RequestBodyReference
				if err := json.Unmarshal(stageResponse.Body.Bytes(), &reference); err != nil {
					providerDone <- err
					return
				}
				create["request_ref"] = reference.RequestRef
			}
			createBody, _ := json.Marshal(create)
			createRequest := httptest.NewRequest(http.MethodPost, "/v1/turns", bytes.NewReader(createBody))
			createRequest.Header.Set("Content-Type", "application/json")
			createResponse := httptest.NewRecorder()
			handler.ServeHTTP(createResponse, createRequest)
			if createResponse.Code != http.StatusCreated {
				providerDone <- fmt.Errorf("create=%d body=%s", createResponse.Code, createResponse.Body.String())
				return
			}
			if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
				providerDone <- err
				return
			}
			waitRequest := httptest.NewRequest(http.MethodGet, "/v1/turns/"+string(created.ID)+"/response", nil)
			waitRequest.SetPathValue("turnID", string(created.ID))
			waitResponse := httptest.NewRecorder()
			handler.ServeHTTP(waitResponse, waitRequest)
			if waitResponse.Code != http.StatusOK {
				providerDone <- fmt.Errorf("wait=%d body=%s", waitResponse.Code, waitResponse.Body.String())
				return
			}
			if err := json.Unmarshal(waitResponse.Body.Bytes(), &received); err != nil {
				providerDone <- err
				return
			}
			providerDone <- nil
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		offer, err := controller.Next(ctx, runtime.RuntimeID)
		cancel()
		if err != nil {
			t.Fatalf("sequence=%d next: %v", sequence, err)
		}
		if offer.Sequence != sequence || offer.RequestDigest != digest || !bytes.Equal(offer.RequestPayload, payload) {
			t.Fatalf("sequence=%d offer=%+v payload_bytes=%d", sequence, offer.Record, len(offer.RequestPayload))
		}
		responsePayload := json.RawMessage(`{"finish_reason":"stop","text":"ok","tool_calls":[],"usage":null}`)
		if _, err := controller.Respond(context.Background(), ResponseSubmission{
			RuntimeID: runtime.RuntimeID, TurnID: offer.TurnID, ExpectedSequence: sequence,
			RequestDigest: digest, Payload: responsePayload,
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-providerDone:
			if err != nil {
				metrics := callDriverRaw(t, handler, http.MethodGet, "/v1/metrics", nil, nil, http.StatusOK)
				t.Fatalf("sequence=%d provider=%v metrics=%v", sequence, err, metrics)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("sequence=%d provider timed out", sequence)
		}
		if created.ID != offer.TurnID || received.TurnID != offer.TurnID || received.RequestDigest != digest {
			t.Fatalf("sequence=%d created=%+v received=%+v offer=%+v", sequence, created, received, offer.Record)
		}
	}
}
