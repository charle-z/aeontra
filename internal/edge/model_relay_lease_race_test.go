package edge

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestModelRelayTwoLeaseIDsClaimExactlyOneRuntime(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	devices, device, privateKey := openPairedRelayDevice(t, now, "relay-lease-race")
	turns, err := modelturn.OpenStore(modelturn.StoreConfig{Root: filepath.Join(t.TempDir(), "turns"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turns.Close() })

	goal := []byte("claim this runtime exactly once")
	goalBody, err := turns.StageRuntimeGoal(t.Context(), goal, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime, created, err := turns.StartBoundRuntime(t.Context(), modelturn.BoundRuntimeRequest{
		DeviceID: device.ID, WorkspaceID: "ws_78787878787878787878787878787878",
		Controller: modelturn.ControllerRemoteEdge, GoalSummary: modelturn.GoalSummary(goal),
		GoalRef: goalBody.BodyRef, GoalDigest: goalBody.ContentDigest,
		IdempotencyKeyDigest: modelturn.IdempotencyDigest("relay-lease-race"), TTL: time.Minute,
	})
	if err != nil || !created {
		t.Fatalf("runtime=%+v created=%t err=%v", runtime, created, err)
	}

	handler := NewHTTPHandler(devices, turns)
	leaseIDs := []string{
		"el_71717171717171717171717171717171",
		"el_72727272727272727272727272727272",
	}
	requests := make([]*http.Request, len(leaseIDs))
	for index, leaseID := range leaseIDs {
		body, err := json.Marshal(modelRuntimeLeaseRequest{LeaseID: leaseID, WaitSeconds: 1})
		if err != nil {
			t.Fatal(err)
		}
		signed := SignedRequest{
			DeviceID: device.ID, Timestamp: now.Unix(),
			Nonce:  "relay-nonce-lease-race-" + string(rune('a'+index)),
			Method: http.MethodPost, Path: modelRuntimeLeasePath, Body: body,
		}
		signed.Signature = ed25519.Sign(privateKey, signed.Canonical())
		requests[index] = signedHTTPRequest(signed)
	}

	start := make(chan struct{})
	responses := make([]*httptest.ResponseRecorder, len(requests))
	var wait sync.WaitGroup
	for index := range requests {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, requests[index])
			responses[index] = response
		}()
	}
	close(start)
	wait.Wait()

	okCount := 0
	noContentCount := 0
	for _, response := range responses {
		switch response.Code {
		case http.StatusOK:
			okCount++
			var leased modelRuntimeLeaseResponse
			if err := json.Unmarshal(response.Body.Bytes(), &leased); err != nil {
				t.Fatal(err)
			}
			if leased.RuntimeID != runtime.RuntimeID || leased.DeviceID != device.ID || leased.WorkspaceID != runtime.WorkspaceID {
				t.Fatalf("leased=%+v", leased)
			}
		case http.StatusNoContent:
			noContentCount++
		default:
			t.Fatalf("unexpected status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if okCount != 1 || noContentCount != 1 {
		t.Fatalf("lease race statuses: ok=%d no_content=%d", okCount, noContentCount)
	}

	leasedRuntime, err := turns.Runtime(t.Context(), runtime.RuntimeID)
	if err != nil || leasedRuntime.State != modelturn.RuntimeStateStarting || leasedRuntime.Status != modelturn.RuntimeRunning {
		t.Fatalf("runtime=%+v err=%v", leasedRuntime, err)
	}
	var receipts int
	if err := devices.db.QueryRow(`SELECT COUNT(*) FROM edge_model_runtime_leases WHERE runtime_id=?`, runtime.RuntimeID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("lease receipts=%d want=1", receipts)
	}

	var winnerLeaseID string
	if err := devices.db.QueryRow(`SELECT lease_id FROM edge_model_runtime_leases WHERE runtime_id=?`, runtime.RuntimeID).Scan(&winnerLeaseID); err != nil {
		t.Fatal(err)
	}
	if winnerLeaseID != leaseIDs[0] && winnerLeaseID != leaseIDs[1] {
		t.Fatalf("unexpected winning lease id=%q", winnerLeaseID)
	}

	replayBody, _ := json.Marshal(modelRuntimeLeaseRequest{LeaseID: winnerLeaseID, WaitSeconds: 1})
	replay := performSignedRequest(t, handler, device.ID, privateKey, now, "relay-nonce-lease-race-replay", http.MethodPost, modelRuntimeLeasePath, replayBody)
	if replay.Code != http.StatusOK {
		t.Fatalf("winning receipt replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayed modelRuntimeLeaseResponse
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil || replayed.RuntimeID != runtime.RuntimeID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
}
