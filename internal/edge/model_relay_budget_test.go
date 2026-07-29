package edge

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestSignedModelRelayLeaseReturnsRequestedExecutionBudget(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-budget")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })
	goal := []byte("preserve the requested execution budget")
	body, err := turns.StageRuntimeGoal(t.Context(), goal, modelturn.RemoteRuntimeStartupTTL)
	if err != nil {
		t.Fatal(err)
	}
	const executionTTL = 240 * time.Second
	runtime, created, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_12121212121212121212121212121212",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: body.BodyRef, GoalDigest: body.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-runtime-budget"),
		TTL:                  modelturn.RemoteRuntimeStartupTTL, ExecutionTTL: executionTTL,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%t err=%v", runtime, created, err)
	}
	handler := NewHTTPHandler(devices, turns)
	response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-budget-0001", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_12121212121212121212121212121212", WaitSeconds: 1})
	if response.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", response.Code, response.Body.String())
	}
	var lease modelRuntimeLeaseResponse
	if err := json.Unmarshal(response.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	if lease.RuntimeID != runtime.RuntimeID || lease.TimeoutSeconds != int(executionTTL/time.Second) {
		t.Fatalf("lease=%+v", lease)
	}
}
