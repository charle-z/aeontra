package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestSignedModelRelayLeaseTurnWaitAndCompletion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-primary")
	_, other, otherKey := openSecondRelayDevice(t, devices, now, "relay-secondary")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })

	goal := []byte("Fix the repository through OpenCode")
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, created, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_0123456789abcdef0123456789abcdef",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-runtime-0001"), TTL: 10 * time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%t err=%v", runtime, created, err)
	}
	handler := NewHTTPHandler(devices, turns)

	lease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-lease-0001", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_01010101010101010101010101010101", WaitSeconds: 1})
	if lease.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", lease.Code, lease.Body.String())
	}
	var leased modelRuntimeLeaseResponse
	if err := json.Unmarshal(lease.Body.Bytes(), &leased); err != nil {
		t.Fatal(err)
	}
	if leased.RuntimeID != runtime.RuntimeID || leased.DeviceID != device.ID || leased.WorkspaceID != runtime.WorkspaceID || leased.Goal != string(goal) || leased.GoalDigest != goalBody.ContentDigest || leased.ProviderProfile != providerProfile || leased.Controller != modelturn.ControllerRemoteEdge {
		t.Fatalf("lease=%+v", leased)
	}
	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-started-0001", modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
	if started.Code != http.StatusOK {
		t.Fatalf("started status=%d body=%s", started.Code, started.Body.String())
	}

	payload := json.RawMessage(`{"messages":[{"content":"read calc.go","role":"user"}],"tools":[{"name":"read"}]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	createRequest := modelTurnCreateRequest{
		CreateID: "ec_0123456789abcdef0123456789abcdef", Sequence: 1, RequestDigest: digest,
		Payload: payload, OfferedTools: []modelturn.ToolDefinition{{ID: "tool-read", Name: "read", Schema: json.RawMessage(`{"type":"object"}`)}},
		TTLMillis: int64((5 * time.Minute) / time.Millisecond),
	}
	turnPath := modelRuntimePrefix + runtime.RuntimeID + "/turns"
	createdTurn := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-create-0001", turnPath, createRequest)
	if createdTurn.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdTurn.Code, createdTurn.Body.String())
	}
	var turn modelturn.Turn
	if err := json.Unmarshal(createdTurn.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	if turn.RuntimeID != runtime.RuntimeID || turn.Sequence != 1 || turn.RequestDigest != digest || !strings.HasPrefix(turn.RequestRef, "mb_") {
		t.Fatalf("turn=%+v", turn)
	}
	retry := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-create-0002", turnPath, createRequest)
	if retry.Code != http.StatusOK {
		t.Fatalf("create retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	var replayed modelturn.Turn
	if err := json.Unmarshal(retry.Body.Bytes(), &replayed); err != nil || replayed.ID != turn.ID || replayed.RequestRef != turn.RequestRef {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}

	wrongDevice := signedRelayRequest(t, handler, other.ID, otherKey, now, "relay-nonce-wrong-device-0001", turnPath, createRequest)
	if wrongDevice.Code != http.StatusNotFound {
		t.Fatalf("wrong device status=%d body=%s", wrongDevice.Code, wrongDevice.Body.String())
	}

	if _, err := turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: 1, RequestDigest: digest,
		Payload:     json.RawMessage(`{"finish_reason":"tool_calls","tool_calls":[{"id":"call-read","name":"read","arguments":{"filePath":"calc.go"}}]}`),
		UsedToolIDs: []string{"tool-read"},
	}); err != nil {
		t.Fatal(err)
	}
	waitPath := modelRuntimePrefix + runtime.RuntimeID + "/turns/" + string(turn.ID) + "/wait"
	waitRequest := modelTurnWaitRequest{WaitID: "ew_0123456789abcdef0123456789abcdef", TimeoutSeconds: 2}
	wait := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-wait-0001", waitPath, waitRequest)
	if wait.Code != http.StatusOK {
		t.Fatalf("wait status=%d body=%s", wait.Code, wait.Body.String())
	}
	var response modelturn.ModelResponse
	if err := json.Unmarshal(wait.Body.Bytes(), &response); err != nil || response.TurnID != turn.ID || response.Sequence != 1 || response.RequestDigest != digest {
		t.Fatalf("response=%+v err=%v", response, err)
	}
	waitReplay := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-wait-0002", waitPath, waitRequest)
	if waitReplay.Code != http.StatusOK || waitReplay.Body.String() != wait.Body.String() {
		t.Fatalf("wait replay status=%d body=%s", waitReplay.Code, waitReplay.Body.String())
	}
	otherWait := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-wait-0003", waitPath, modelTurnWaitRequest{WaitID: "ew_ffffffffffffffffffffffffffffffff", TimeoutSeconds: 2})
	if otherWait.Code != http.StatusConflict {
		t.Fatalf("second wait id status=%d body=%s", otherWait.Code, otherWait.Body.String())
	}

	completed := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-completed-0001", modelRuntimePrefix+runtime.RuntimeID+"/completed", modelRuntimeLifecycleRequest{ResultRef: "rs_0123456789abcdef0123456789abcdef"})
	if completed.Code != http.StatusOK {
		t.Fatalf("completed status=%d body=%s", completed.Code, completed.Body.String())
	}
	final, err := turns.RuntimeForDevice(t.Context(), runtime.RuntimeID, device.ID)
	if err != nil || final.State != modelturn.RuntimeStateCompleted || final.ResultRef == "" {
		t.Fatalf("final=%+v err=%v", final, err)
	}
}

func TestSignedModelRelayRejectsDigestReplayAndTimeoutExpansion(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-guardrails")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	goal := []byte("bounded goal")
	goalBody, _ := turns.StageRuntimeGoal(t.Context(), goal, time.Minute)
	runtime, _, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_abcdefabcdefabcdefabcdefabcdefab", Controller: modelturn.ControllerRemoteEdge,
		GoalSummary: modelturn.GoalSummary(goal), GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-runtime-guardrails"), TTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(devices, turns)
	lease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-guard-lease", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_02020202020202020202020202020202", WaitSeconds: 181})
	if lease.Code != http.StatusBadRequest {
		t.Fatalf("expanded lease status=%d", lease.Code)
	}
	validLease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-guard-lease-valid", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_03030303030303030303030303030303", WaitSeconds: 1})
	if validLease.Code != http.StatusOK {
		t.Fatalf("valid lease status=%d body=%s", validLease.Code, validLease.Body.String())
	}
	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-guard-started", modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
	if started.Code != http.StatusOK {
		t.Fatalf("started status=%d body=%s", started.Code, started.Body.String())
	}
	payload := json.RawMessage(`{"messages":[]}`)
	invalid := modelTurnCreateRequest{
		CreateID: "ec_abcdefabcdefabcdefabcdefabcdefab", Sequence: 1,
		RequestDigest: "sha256:" + strings.Repeat("0", 64), Payload: payload,
		TTLMillis: int64(time.Minute / time.Millisecond),
	}
	path := modelRuntimePrefix + runtime.RuntimeID + "/turns"
	response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-bad-digest-01", path, invalid)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("digest status=%d body=%s", response.Code, response.Body.String())
	}
	body, _ := json.Marshal(modelRuntimeLeaseRequest{LeaseID: "el_04040404040404040404040404040404", WaitSeconds: 1})
	first := performSignedRequest(t, handler, device.ID, privateKey, now, "relay-nonce-replay-0001", http.MethodPost, modelRuntimeLeasePath, body)
	second := performSignedRequest(t, handler, device.ID, privateKey, now, "relay-nonce-replay-0001", http.MethodPost, modelRuntimeLeasePath, body)
	if first.Code == http.StatusUnauthorized || second.Code != http.StatusUnauthorized {
		t.Fatalf("nonce replay first=%d second=%d", first.Code, second.Code)
	}
}

func TestSignedModelRelayLeaseKeepsRuntimeRetryableWhenGoalIsUnavailable(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	clock := now
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-retryable-goal")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })

	goal := []byte("retry the lease when the goal body is temporarily unavailable")
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runtime, created, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-runtime-retryable-goal"), TTL: 10 * time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%t err=%v", runtime, created, err)
	}
	clock = clock.Add(2 * time.Second)
	handler := NewHTTPHandler(devices, turns)

	lease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-retryable-goal", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", WaitSeconds: 1})
	if lease.Code != http.StatusServiceUnavailable {
		t.Fatalf("lease status=%d body=%s", lease.Code, lease.Body.String())
	}
	retry := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-retryable-goal-retry", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", WaitSeconds: 1})
	if retry.Code != http.StatusServiceUnavailable {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	after, err := turns.RuntimeForDevice(t.Context(), runtime.RuntimeID, device.ID)
	if err != nil || after.State != modelturn.RuntimeStateAwaitingEdge || after.Status != modelturn.RuntimeReady {
		t.Fatalf("runtime after retryable lease=%+v err=%v", after, err)
	}
}

func openPairedRelayDevice(t *testing.T, now time.Time, name string) (*Store, Device, ed25519.PrivateKey) {
	t.Helper()
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, device, privateKey := openSecondRelayDevice(t, store, now, name)
	return store, device, privateKey
}

func openSecondRelayDevice(t *testing.T, store *Store, now time.Time, name string) (ed25519.PublicKey, Device, ed25519.PrivateKey) {
	t.Helper()
	code, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.Pair(code, name, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, device, privateKey
}

func signedRelayRequest(t *testing.T, handler http.Handler, deviceID string, privateKey ed25519.PrivateKey, now time.Time, nonce, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return performSignedRequest(t, handler, deviceID, privateKey, now, nonce, http.MethodPost, path, body)
}
