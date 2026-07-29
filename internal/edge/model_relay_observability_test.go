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

func TestSignedModelRelayPersistsSafeRuntimePhases(t *testing.T) {
	now := time.Now().UTC().Add(time.Minute)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-observability")
	root := filepath.Join(t.TempDir(), "turns")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	goal := []byte("private benchmark prompt must not be observable")
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, created, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_11111111111111111111111111111111",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-observability"), TTL: 10 * time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%v err=%v", runtime, created, err)
	}
	handler := NewHTTPHandler(devices, turns)
	lease := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-lease", modelRuntimeLeasePath, modelRuntimeLeaseRequest{LeaseID: "el_11111111111111111111111111111111", WaitSeconds: 1})
	if lease.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", lease.Code, lease.Body.String())
	}

	phasePath := modelRuntimePrefix + runtime.RuntimeID + "/phase"
	outOfOrder := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-out-of-order", phasePath, modelRuntimeLifecycleRequest{Phase: modelturn.RuntimePhaseDriverSocketReady, Count: 1})
	if outOfOrder.Code != http.StatusBadRequest {
		t.Fatalf("out-of-order status=%d body=%s", outOfOrder.Code, outOfOrder.Body.String())
	}
	for index, request := range []modelRuntimeLifecycleRequest{
		{Phase: modelturn.RuntimePhaseLeaseRetry, RetryCategory: modelturn.RuntimeRetryGatewayTimeout, Count: 2},
		{Phase: modelturn.RuntimePhaseLocalPreflightComplete, Count: 1},
	} {
		response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-phase-"+string(rune('a'+index)), phasePath, request)
		if response.Code != http.StatusOK {
			t.Fatalf("phase=%+v status=%d body=%s", request, response.Code, response.Body.String())
		}
	}
	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-started", modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
	if started.Code != http.StatusOK {
		t.Fatalf("started status=%d body=%s", started.Code, started.Body.String())
	}
	for index, request := range []modelRuntimeLifecycleRequest{{Phase: modelturn.RuntimePhaseDriverSocketReady, Count: 1}, {Phase: modelturn.RuntimePhaseOpenCodeProcessStarted, Count: 1}} {
		response := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-post-start-"+string(rune('a'+index)), phasePath, request)
		if response.Code != http.StatusOK {
			t.Fatalf("phase=%+v status=%d body=%s", request, response.Code, response.Body.String())
		}
	}
	invalid := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-observability-private", phasePath, modelRuntimeLifecycleRequest{
		Phase: modelturn.RuntimePhaseDriverSocketReady, RetryCategory: "private-body", Count: 1,
	})
	if invalid.Code != http.StatusConflict || strings.Contains(invalid.Body.String(), "private-body") {
		t.Fatalf("invalid status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	view, err := turns.RuntimeForDevice(t.Context(), runtime.RuntimeID, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []modelturn.RuntimePhase{
		modelturn.RuntimePhaseCreated,
		modelturn.RuntimePhaseLeaseAssigned,
		modelturn.RuntimePhaseLeaseRetry,
		modelturn.RuntimePhaseLocalPreflightComplete,
		modelturn.RuntimePhaseStartedConfirmed,
		modelturn.RuntimePhaseDriverSocketReady,
		modelturn.RuntimePhaseOpenCodeProcessStarted,
	}
	if len(view.Phases) != len(want) {
		t.Fatalf("phases=%+v", view.Phases)
	}
	for index, phase := range view.Phases {
		if phase.Phase != want[index] || phase.DurationMS < 0 || phase.SinceCreatedMS < 0 {
			t.Fatalf("index=%d phase=%+v", index, phase)
		}
	}
	if view.Phases[2].RetryCategory != modelturn.RuntimeRetryGatewayTimeout || view.Phases[2].Count != 2 {
		t.Fatalf("retry=%+v", view.Phases[2])
	}
	encoded, err := json.Marshal(view.Phases)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private benchmark", "prompt", "command", "arguments", "credential", "private-body"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("phase metadata leaked %q: %s", forbidden, encoded)
		}
	}
	if len(encoded) > 8192 {
		t.Fatalf("phase metadata bytes=%d", len(encoded))
	}
	if err := turns.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := modelturn.OpenStore(modelturn.StoreConfig{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.RuntimeForDevice(t.Context(), runtime.RuntimeID, device.ID)
	if err != nil || len(restored.Phases) != len(want) {
		t.Fatalf("restored=%+v err=%v", restored.Phases, err)
	}
}
