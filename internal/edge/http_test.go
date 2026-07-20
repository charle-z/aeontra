package edge

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHTTPPairConsumesCodeAndReturnsOnlyDeviceIdentity(t *testing.T) {
	store := openHTTPTestStore(t)
	code, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{
		"code":       code,
		"name":       "wsl-development",
		"public_key": EncodePublicKey(publicKey),
	})

	response := httptest.NewRecorder()
	NewHTTPHandler(store).ServeHTTP(response, httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), code) || strings.Contains(response.Body.String(), EncodePublicKey(publicKey)) {
		t.Fatal("pairing response exposed a credential")
	}
	var device Device
	if err := json.Unmarshal(response.Body.Bytes(), &device); err != nil {
		t.Fatal(err)
	}
	if device.ID == "" || device.State != StateActive {
		t.Fatalf("device=%+v", device)
	}

	replay := httptest.NewRecorder()
	NewHTTPHandler(store).ServeHTTP(replay, httptest.NewRequest(http.MethodPost, PairPath, bytes.NewReader(body)))
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
}

func TestHTTPPairIsStrictAndBounded(t *testing.T) {
	store := openHTTPTestStore(t)
	handler := NewHTTPHandler(store)
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "unknown field", body: `{"code":"x","name":"wsl","public_key":"x","extra":true}`, want: http.StatusBadRequest},
		{name: "trailing json", body: `{"code":"x","name":"wsl","public_key":"x"}{}`, want: http.StatusBadRequest},
		{name: "oversized", body: `{"code":"` + strings.Repeat("x", maxPairBody) + `"}`, want: http.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, PairPath, strings.NewReader(test.body)))
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodGet, PairPath, nil))
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("status=%d allow=%q", method.Code, method.Header().Get("Allow"))
	}
}

func TestHTTPControlKeyAndOperationLeaseAreServerSigned(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(store)
	now := time.Now().UTC()
	keyResponse := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-control-key-000001", http.MethodPost, "/edge/v1/control-key", []byte(`{}`))
	if keyResponse.Code != http.StatusOK {
		t.Fatalf("control key status=%d body=%s", keyResponse.Code, keyResponse.Body.String())
	}
	var keyBody controlKeyResponse
	if json.Unmarshal(keyResponse.Body.Bytes(), &keyBody) != nil || keyBody.PublicKey != EncodePublicKey(store.ControlPublicKey()) {
		t.Fatalf("control key response=%s", keyResponse.Body.String())
	}
	operation, _, err := store.CreateOperation(device.ID, OperationBundleUpdate, OperationRequest{Release: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	leaseResponse := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-control-lease-0001", http.MethodPost, "/edge/v1/operations/lease", []byte(`{"lease_seconds":60}`))
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", leaseResponse.Code, leaseResponse.Body.String())
	}
	var lease OperationLease
	if json.Unmarshal(leaseResponse.Body.Bytes(), &lease) != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease response=%s", leaseResponse.Body.String())
	}
	signature, err := base64.RawURLEncoding.DecodeString(lease.ControlSignature)
	if err != nil || !ed25519.Verify(store.ControlPublicKey(), lease.ControlCanonical(), signature) {
		t.Fatal("operation lease does not carry a valid server signature")
	}
}

func TestSignedHTTPAuthenticationUsesExactRequestBodyAndRejectsReplay(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "wsl-development", publicKey)
	if err != nil {
		t.Fatal(err)
	}

	protected := store.RequireDevice(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if DeviceFromContext(r.Context()).ID != device.ID {
			t.Fatal("authenticated device missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	body := []byte(`{"ready":true}`)
	request := SignedRequest{DeviceID: device.ID, Timestamp: now.Unix(), Nonce: "nonce-0123456789abcdef", Method: http.MethodPost, Path: "/edge/v1/heartbeat", Body: body}
	request.Signature = ed25519.Sign(privateKey, request.Canonical())

	first := httptest.NewRecorder()
	protected.ServeHTTP(first, signedHTTPRequest(request))
	if first.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	protected.ServeHTTP(second, signedHTTPRequest(request))
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d", second.Code)
	}
}

func TestHTTPTaskLeaseHeartbeatAndCompletionUseSignedDeviceProtocol(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "wsl-development", publicKey)
	task, _, err := store.CreateTask(device.ID, validTaskSpec("http-task-0001"))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(store)

	leaseBody := []byte(`{"workcell":"development","holder":"agent-session-0001","lease_seconds":60}`)
	leaseResponse := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-http-lease-0001", http.MethodPost, "/edge/v1/tasks/lease", leaseBody)
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", leaseResponse.Code, leaseResponse.Body.String())
	}
	var lease Lease
	if err := json.Unmarshal(leaseResponse.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Task.ID != task.ID {
		t.Fatalf("lease=%+v", lease)
	}

	heartbeatBody, _ := json.Marshal(heartbeatRequest{LeaseID: lease.LeaseID, LeaseSeconds: 60})
	heartbeat := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-http-heartbeat-0001", http.MethodPost, "/edge/v1/tasks/"+task.ID+"/heartbeat", heartbeatBody)
	if heartbeat.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", heartbeat.Code, heartbeat.Body.String())
	}

	completeBody, _ := json.Marshal(completionRequest{LeaseID: lease.LeaseID, Outcome: OutcomeSucceeded, Summary: "validated"})
	complete := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-http-complete-0001", http.MethodPost, "/edge/v1/tasks/"+task.ID+"/complete", completeBody)
	if complete.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", complete.Code, complete.Body.String())
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/edge/v1/tasks/lease", strings.NewReader(`{}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.Code)
	}
}

func TestHTTPLeaseReturnsNoContentWhenQueueIsEmpty(t *testing.T) {
	now := time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "wsl-development", publicKey)
	body := []byte(`{"workcell":"development","holder":"agent-session-0001","lease_seconds":60}`)
	response := performSignedRequest(t, NewHTTPHandler(store), device.ID, privateKey, now, "nonce-http-empty-0001", http.MethodPost, "/edge/v1/tasks/lease", body)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func openHTTPTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func signedHTTPRequest(request SignedRequest) *http.Request {
	httpRequest := httptest.NewRequest(request.Method, request.Path, bytes.NewReader(request.Body))
	httpRequest.Header.Set(HeaderDevice, request.DeviceID)
	httpRequest.Header.Set(HeaderTimestamp, strconv.FormatInt(request.Timestamp, 10))
	httpRequest.Header.Set(HeaderNonce, request.Nonce)
	httpRequest.Header.Set(HeaderSignature, encodeSignature(request.Signature))
	return httpRequest
}

func performSignedRequest(t *testing.T, handler http.Handler, deviceID string, privateKey ed25519.PrivateKey, now time.Time, nonce, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	signed := SignedRequest{DeviceID: deviceID, Timestamp: now.Unix(), Nonce: nonce, Method: method, Path: path, Body: body}
	signed.Signature = ed25519.Sign(privateKey, signed.Canonical())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, signedHTTPRequest(signed))
	return response
}
