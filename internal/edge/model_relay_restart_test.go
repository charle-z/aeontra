package edge

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRelayTimeoutReconnectPreservesExactTurn(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-restart")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "restart")
	handler := NewHTTPHandler(devices, turns)
	turn, digest := createRelayTurn(t, handler, runtime.RuntimeID, device.ID, privateKey, now, "ec_11111111111111111111111111111111", "relay-nonce-restart-create")
	before, err := turns.Get(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}

	waitPath := modelRuntimePrefix + runtime.RuntimeID + "/turns/" + string(turn.ID) + "/wait"
	waitInput := modelTurnWaitRequest{WaitID: "ew_11111111111111111111111111111111", TimeoutSeconds: 1}
	timedOut := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-restart-wait-1", waitPath, waitInput)
	if timedOut.Code != http.StatusNoContent || timedOut.Header().Get("Retry-After") != "1" {
		t.Fatalf("timeout status=%d retry=%q body=%s", timedOut.Code, timedOut.Header().Get("Retry-After"), timedOut.Body.String())
	}
	disconnected, err := turns.Get(t.Context(), turn.ID)
	if err != nil || disconnected.Status != modelturn.StatusDisconnected || disconnected.TurnID != before.TurnID || disconnected.Sequence != before.Sequence || disconnected.RequestDigest != before.RequestDigest || disconnected.RequestRef != before.RequestRef {
		t.Fatalf("disconnected=%+v before=%+v err=%v", disconnected, before, err)
	}
	disconnectedRuntime, err := turns.RuntimeForDevice(t.Context(), runtime.RuntimeID, device.ID)
	if err != nil || disconnectedRuntime.State != modelturn.RuntimeStateDisconnected {
		t.Fatalf("runtime=%+v err=%v", disconnectedRuntime, err)
	}

	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-restart-started", modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
	if started.Code != http.StatusOK {
		t.Fatalf("restart status=%d body=%s", started.Code, started.Body.String())
	}
	resumed, err := turns.Get(t.Context(), turn.ID)
	if err != nil || resumed.Status != modelturn.StatusAwaitingModel || resumed.TurnID != before.TurnID || resumed.Sequence != before.Sequence || resumed.RequestDigest != before.RequestDigest || resumed.RequestRef != before.RequestRef {
		t.Fatalf("resumed=%+v before=%+v err=%v", resumed, before, err)
	}
	if _, err := turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence,
		RequestDigest: digest, Payload: json.RawMessage(`{"finish_reason":"stop","text":"resumed"}`),
	}); err != nil {
		t.Fatal(err)
	}
	completedWait := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-restart-wait-2", waitPath, waitInput)
	if completedWait.Code != http.StatusOK {
		t.Fatalf("resumed wait status=%d body=%s", completedWait.Code, completedWait.Body.String())
	}
	consumed, err := turns.Get(t.Context(), turn.ID)
	if err != nil || consumed.Status != modelturn.StatusConsumed || consumed.TurnID != before.TurnID || consumed.Sequence != before.Sequence || consumed.RequestDigest != before.RequestDigest || consumed.RequestRef != before.RequestRef {
		t.Fatalf("consumed=%+v before=%+v err=%v", consumed, before, err)
	}
}

func TestModelRelayAllowsOneActiveWaitPerDeviceRuntime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-single-wait")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "single-wait")
	handler := NewHTTPHandler(devices, turns)
	turn, digest := createRelayTurn(t, handler, runtime.RuntimeID, device.ID, privateKey, now, "ec_22222222222222222222222222222222", "relay-nonce-single-create")
	waitPath := modelRuntimePrefix + runtime.RuntimeID + "/turns/" + string(turn.ID) + "/wait"
	waitInput := modelTurnWaitRequest{WaitID: "ew_22222222222222222222222222222222", TimeoutSeconds: 3}
	firstDone := make(chan int, 1)
	go func() {
		response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-single-wait-1", waitPath, waitInput)
		firstDone <- response.Code
	}()
	time.Sleep(150 * time.Millisecond)
	second := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-single-wait-2", waitPath, waitInput)
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent wait status=%d body=%s", second.Code, second.Body.String())
	}
	if _, err := turns.Respond(t.Context(), modelturn.ResponseSubmission{
		RuntimeID: runtime.RuntimeID, TurnID: turn.ID, ExpectedSequence: turn.Sequence,
		RequestDigest: digest, Payload: json.RawMessage(`{"finish_reason":"stop","text":"done"}`),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-firstDone:
		if code != http.StatusOK {
			t.Fatalf("first wait status=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first wait did not unblock")
	}
}

func createAndLeaseRelayRuntime(t *testing.T, devices *Store, turns *modelturn.Store, device Device, privateKey []byte, now time.Time, suffix string) modelturn.Runtime {
	t.Helper()
	goal := []byte("relay goal " + suffix)
	body, err := turns.StageRuntimeGoal(t.Context(), goal, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_33333333333333333333333333333333",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: body.BodyRef, GoalDigest: body.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("runtime-" + suffix), TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(devices, turns)
	lease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-lease-"+suffix, modelRuntimeLeasePath, modelRuntimeLeaseRequest{WaitSeconds: 1})
	if lease.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", lease.Code, lease.Body.String())
	}
	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-start-"+suffix, modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
	if started.Code != http.StatusOK {
		t.Fatalf("started status=%d body=%s", started.Code, started.Body.String())
	}
	return runtime
}

func createRelayTurn(t *testing.T, handler http.Handler, runtimeID, deviceID string, privateKey []byte, now time.Time, createID, nonce string) (modelturn.Turn, string) {
	t.Helper()
	payload := json.RawMessage(`{"messages":[{"content":"test","role":"user"}],"tools":[]}`)
	digest, err := modelturn.ExactPayloadDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	response := signedRelayRequest(t, handler, deviceID, privateKey, now, nonce, modelRuntimePrefix+runtimeID+"/turns", modelTurnCreateRequest{
		CreateID: createID, Sequence: 1, RequestDigest: digest, Payload: payload,
		TTLMillis: int64((2 * time.Minute) / time.Millisecond),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var turn modelturn.Turn
	if err := json.Unmarshal(response.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	return turn, digest
}
