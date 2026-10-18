package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRelaySurvivesIndependentStoreAndHandlerRestart(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	root := t.TempDir()
	deviceRoot := filepath.Join(root, "edge")
	turnRoot := filepath.Join(root, "turns")
	devices, err := Open(Config{Root: deviceRoot, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	code, err := devices.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := devices.Pair(code, "relay-connection-restart", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: turnRoot, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	runtime := createAndLeaseRelayRuntime(t, devices, turns, device, privateKey, now, "connection-restart")
	handler := NewHTTPHandler(devices, turns)
	turn, digest := createRelayTurn(t, handler, runtime.RuntimeID, device.ID, privateKey, now, "ec_51515151515151515151515151515151", "relay-nonce-connection-create")
	before, err := turns.Get(t.Context(), turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitPath := modelRuntimePrefix + runtime.RuntimeID + "/turns/" + string(turn.ID) + "/wait"
	waitInput := modelTurnWaitRequest{WaitID: "ew_51515151515151515151515151515151", TimeoutSeconds: 1}
	timedOut := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-connection-wait-1", waitPath, waitInput)
	if timedOut.Code != http.StatusNoContent {
		t.Fatalf("timeout status=%d body=%s", timedOut.Code, timedOut.Body.String())
	}
	if err := turns.Close(); err != nil {
		t.Fatal(err)
	}
	if err := devices.Close(); err != nil {
		t.Fatal(err)
	}

	devices, err = Open(Config{Root: deviceRoot, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer devices.Close()
	turns, err = modelturn.OpenStore(modelturn.StoreConfig{Root: turnRoot, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer turns.Close()
	handler = NewHTTPHandler(devices, turns)
	started := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-connection-started", modelRuntimePrefix+runtime.RuntimeID+"/started", modelRuntimeLifecycleRequest{})
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
	consumedResponse := signedRelayRequest(t, handler, device.ID, privateKey, now, "relay-nonce-connection-wait-2", waitPath, waitInput)
	if consumedResponse.Code != http.StatusOK {
		t.Fatalf("resumed wait status=%d body=%s", consumedResponse.Code, consumedResponse.Body.String())
	}
	consumed, err := turns.Get(t.Context(), turn.ID)
	if err != nil || consumed.Status != modelturn.StatusConsumed || consumed.TurnID != before.TurnID || consumed.Sequence != before.Sequence || consumed.RequestDigest != before.RequestDigest || consumed.RequestRef != before.RequestRef {
		t.Fatalf("consumed=%+v before=%+v err=%v", consumed, before, err)
	}
}
