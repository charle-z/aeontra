package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRelayRejectsInvalidAuthenticationAndBindings(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer turns.Close()
	device, privateKey := pairRelayDevice(t, devices, "relay-security-primary")
	otherDevice, otherPrivateKey := pairRelayDevice(t, devices, "relay-security-other")
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "security-matrix")
	handler := NewHTTPHandler(devices, turns)

	t.Run("incorrect signature", func(t *testing.T) {
		body, _ := json.Marshal(modelRuntimeLifecycleRequest{})
		request := SignedRequest{
			DeviceID: device.ID, Timestamp: now.Unix(), Nonce: "relay-nonce-bad-signature", Method: http.MethodPost,
			Path: modelRuntimePrefix + runtime.RuntimeID + "/heartbeat", Body: body,
		}
		request.Signature = ed25519.Sign(otherPrivateKey, request.Canonical())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, signedHTTPRequest(request))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("stale timestamp", func(t *testing.T) {
		body, _ := json.Marshal(modelRuntimeLifecycleRequest{})
		request := SignedRequest{
			DeviceID: device.ID, Timestamp: now.Add(-10 * time.Minute).Unix(), Nonce: "relay-nonce-stale-timestamp", Method: http.MethodPost,
			Path: modelRuntimePrefix + runtime.RuntimeID + "/heartbeat", Body: body,
		}
		request.Signature = ed25519.Sign(privateKey, request.Canonical())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, signedHTTPRequest(request))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("body changed after signing", func(t *testing.T) {
		original := []byte(`{"result_ref":""}`)
		request := SignedRequest{
			DeviceID: device.ID, Timestamp: now.Unix(), Nonce: "relay-nonce-mutated-body", Method: http.MethodPost,
			Path: modelRuntimePrefix + runtime.RuntimeID + "/heartbeat", Body: original,
		}
		request.Signature = ed25519.Sign(privateKey, request.Canonical())
		httpRequest := signedHTTPRequest(request)
		httpRequest.Body = ioNopCloser{bytes.NewReader([]byte(`{"result_ref":"rs_11111111111111111111111111111111"}`))}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httpRequest)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("incorrect device", func(t *testing.T) {
		response := signedRelayRequest(t, handler, otherDevice.ID, otherPrivateKey, now, "relay-nonce-wrong-device", modelRuntimePrefix+runtime.RuntimeID+"/heartbeat", modelRuntimeLifecycleRequest{})
		if response.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("incorrect runtime", func(t *testing.T) {
		response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-wrong-runtime", modelRuntimePrefix+"mr_99999999999999999999999999999999/heartbeat", modelRuntimeLifecycleRequest{})
		if response.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("incorrect digest", func(t *testing.T) {
		payload := json.RawMessage(`{"messages":[{"role":"user","content":"bounded"}],"tools":[]}`)
		input := modelTurnCreateRequest{
			CreateID: "ec_61616161616161616161616161616161", Sequence: 1,
			RequestDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			Payload:       payload, TTLMillis: int64(time.Minute / time.Millisecond),
		}
		response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-bad-digest", modelRuntimePrefix+runtime.RuntimeID+"/turns", input)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		stats, err := turns.Stats(context.Background())
		if err != nil || stats.TurnCount != 0 {
			t.Fatalf("stats=%+v err=%v", stats, err)
		}
	})
}

func TestModelRelayRevocationCancelsActiveWait(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer turns.Close()
	device, privateKey := pairRelayDevice(t, devices, "relay-revocation")
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "revocation")
	handler := NewHTTPHandler(devices, turns)
	turn, _ := createRelayTurn(t, handler, runtime.RuntimeID, device.ID, privateKey, now, "ec_62626262626262626262626262626262", "relay-nonce-revocation-create")
	path := modelRuntimePrefix + runtime.RuntimeID + "/turns/" + string(turn.ID) + "/wait"
	input := modelTurnWaitRequest{WaitID: "ew_62626262626262626262626262626262", TimeoutSeconds: 30}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-revocation-wait", path, input)
	}()
	time.Sleep(150 * time.Millisecond)
	if err := devices.Revoke(device.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	case <-time.After(7 * time.Second):
		t.Fatal("revocation did not cancel active wait")
	}
	record, err := turns.Get(t.Context(), turn.ID)
	if err != nil || record.Status == modelturn.StatusConsumed {
		t.Fatalf("record=%+v err=%v", record, err)
	}
}

func pairRelayDevice(t *testing.T, devices *Store, name string) (Device, ed25519.PrivateKey) {
	t.Helper()
	code, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := devices.Pair(code, name, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return device, privateKey
}

type ioNopCloser struct {
	*bytes.Reader
}

func (ioNopCloser) Close() error { return nil }

var _ = strconv.IntSize
