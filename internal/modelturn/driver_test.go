package modelturn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDriverInlineAndReferencedRequestsShareExactDigest(t *testing.T) {
	store := openDriverStore(t, 0)
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	driver, _ := NewDriver(store)

	inline := []byte(`{"model_id":"external","prompt":[{"content":"inline","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`)
	inlineDigest := digestBytes(inline)
	inlineEnvelope := map[string]any{
		"runtime_id":     runtime.RuntimeID,
		"sequence":       1,
		"request_digest": inlineDigest,
		"payload":        json.RawMessage(inline),
		"ttl_ms":         60000,
	}
	inlineResponse := callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", inlineEnvelope, nil, http.StatusCreated)
	var inlineTurn Turn
	decodeJSONMap(t, inlineResponse, &inlineTurn)
	inlineOffer, err := store.nextOnce(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if inlineTurn.RequestDigest != inlineDigest || !bytes.Equal(inlineOffer.RequestPayload, inline) {
		t.Fatalf("inline turn=%+v payload=%s", inlineTurn, inlineOffer.RequestPayload)
	}
	if err := store.Cancel(context.Background(), inlineTurn.ID); err != nil {
		t.Fatal(err)
	}

	large := []byte(`{"model_id":"external","prompt":[{"content":"` + strings.Repeat("x", int(MaxInlineRequestBytes)+4096) + `","role":"user"}],"protocol_version":"mcp-devbox.model-turn.v1"}`)
	largeDigest := digestBytes(large)
	stageHeaders := map[string]string{
		"Content-Type":      "application/json",
		requestDigestHeader: largeDigest,
		requestTTLHeader:    "60000",
	}
	staged := callDriverRaw(t, driver.Handler(), http.MethodPost, "/v1/request-bodies", large, stageHeaders, http.StatusCreated)
	var reference RequestBodyReference
	decodeJSONMap(t, staged, &reference)
	if reference.RequestDigest != largeDigest || reference.ContentBytes != int64(len(large)) {
		t.Fatalf("reference=%+v", reference)
	}

	referencedEnvelope := map[string]any{
		"runtime_id":     runtime.RuntimeID,
		"sequence":       2,
		"request_digest": largeDigest,
		"request_ref":    reference.RequestRef,
		"ttl_ms":         60000,
	}
	referenced := callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", referencedEnvelope, nil, http.StatusCreated)
	var referencedTurn Turn
	decodeJSONMap(t, referenced, &referencedTurn)
	offer, err := store.nextOnce(context.Background(), runtime.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	if referencedTurn.RequestDigest != largeDigest || offer.RequestRef != reference.RequestRef || !bytes.Equal(offer.RequestPayload, large) {
		t.Fatalf("referenced turn=%+v offer=%+v", referencedTurn, offer.Record)
	}
}

func TestReferencedBodyIsImmutableAndSingleUse(t *testing.T) {
	store := openDriverStore(t, 0)
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body := json.RawMessage(`{"text":"` + strings.Repeat("a", int(MaxInlineRequestBytes)+1) + `"}`)
	reference, err := store.StageRequestBody(context.Background(), body, true, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE turn_bodies SET content=? WHERE body_ref=?`, []byte(`{"changed":true}`), reference.RequestRef); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("body mutation was not rejected: %v", err)
	}
	turn, err := store.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID: runtime.RuntimeID, Sequence: 1, RequestRef: reference.RequestRef, RequestDigest: reference.RequestDigest, TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if turn.RequestDigest != reference.RequestDigest {
		t.Fatalf("turn=%+v", turn)
	}
	if _, err := store.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID: runtime.RuntimeID, Sequence: 2, RequestRef: reference.RequestRef, RequestDigest: reference.RequestDigest, TTL: time.Minute,
	}); err != ErrRequestRefConflict {
		t.Fatalf("request reference replay error=%v", err)
	}
	if _, err := store.CreateTurnFromReference(context.Background(), ModelRequest{
		RuntimeID: runtime.RuntimeID, Sequence: 2, RequestRef: reference.RequestRef, RequestDigest: "sha256:" + strings.Repeat("0", 64), TTL: time.Minute,
	}); err != ErrRequestRefConflict {
		t.Fatalf("changed digest error=%v", err)
	}
}

func TestDriverRejectsOversizeBodiesAndQuotaExhaustion(t *testing.T) {
	store := openDriverStore(t, 96<<10)
	driver, _ := NewDriver(store)
	oversize := []byte(`{"text":"` + strings.Repeat("x", int(MaxRequestBodyBytes)+1) + `"}`)
	digest := sha256.Sum256(oversize)
	callDriverRaw(t, driver.Handler(), http.MethodPost, "/v1/request-bodies", oversize, map[string]string{
		"Content-Type":      "application/json",
		requestDigestHeader: "sha256:" + hex.EncodeToString(digest[:]),
	}, http.StatusRequestEntityTooLarge)

	withinLimit := []byte(`{"text":"` + strings.Repeat("y", int(MaxInlineRequestBytes)+4096) + `"}`)
	withinDigest := digestBytes(withinLimit)
	first := callDriverRaw(t, driver.Handler(), http.MethodPost, "/v1/request-bodies", withinLimit, map[string]string{
		"Content-Type": "application/json", requestDigestHeader: withinDigest,
	}, http.StatusCreated)
	if first["request_ref"] == "" {
		t.Fatalf("first reference=%v", first)
	}
	callDriverRaw(t, driver.Handler(), http.MethodPost, "/v1/request-bodies", withinLimit, map[string]string{
		"Content-Type": "application/json", requestDigestHeader: withinDigest,
	}, http.StatusInsufficientStorage)
}

func TestDriverRejectsUnknownFieldsDigestMismatchAndBothPayloadModes(t *testing.T) {
	store := openDriverStore(t, 0)
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	driver, _ := NewDriver(store)
	payload := json.RawMessage(`{"prompt":[]}`)
	callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", map[string]any{
		"runtime_id": runtime.RuntimeID, "sequence": 1, "request_digest": "sha256:" + strings.Repeat("0", 64), "payload": payload, "unknown": true,
	}, nil, http.StatusBadRequest)
	callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", map[string]any{
		"runtime_id": runtime.RuntimeID, "sequence": 1, "request_digest": "sha256:" + strings.Repeat("0", 64), "payload": payload,
	}, nil, http.StatusConflict)
	callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", map[string]any{
		"runtime_id": runtime.RuntimeID, "sequence": 1, "request_digest": digestBytes(payload), "payload": payload, "request_ref": "mb_33333333333333333333333333333333",
	}, nil, http.StatusBadRequest)
}

func TestDriverMetricsContainCountsOnlyNotBodies(t *testing.T) {
	store := openDriverStore(t, 0)
	driver, _ := NewDriver(store)
	secret := "private-prompt-marker"
	runtime, err := store.StartRuntime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"content":"` + secret + `"}`)
	callDriverJSON(t, driver.Handler(), http.MethodPost, "/v1/turns", map[string]any{
		"runtime_id": runtime.RuntimeID, "sequence": 1, "request_digest": digestBytes(payload), "payload": payload,
	}, nil, http.StatusCreated)
	metrics := callDriverRaw(t, driver.Handler(), http.MethodGet, "/v1/metrics", nil, nil, http.StatusOK)
	encoded, _ := json.Marshal(metrics)
	if strings.Contains(string(encoded), secret) || metrics["protocol_version"] != DriverProtocolVersion {
		t.Fatalf("metrics leaked content: %s", encoded)
	}
}

func openDriverStore(t *testing.T, quota int64) *Store {
	t.Helper()
	store, err := OpenStore(StoreConfig{Root: filepath.Join(t.TempDir(), "model-turns"), QuotaBytes: quota})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func callDriverJSON(t *testing.T, handler http.Handler, method, path string, value any, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	return callDriverRaw(t, handler, method, path, body, headers, wantStatus)
}

func callDriverRaw(t *testing.T, handler http.Handler, method, path string, body []byte, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response map[string]any
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response status=%d body=%q: %v", recorder.Code, recorder.Body.String(), err)
		}
	}
	if recorder.Code != wantStatus {
		t.Fatalf("status=%d want=%d response=%v", recorder.Code, wantStatus, response)
	}
	return response
}

func decodeJSONMap(t *testing.T, value map[string]any, target any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatal(err)
	}
}
