package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPOperationProgressAndCancellationUseSignedDeviceProtocol(t *testing.T) {
	now := time.Date(2026, 7, 29, 20, 55, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandler(store)

	leaseResponse := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-lifecycle-lease-01", http.MethodPost, "/edge/v1/operations/lease", []byte(`{"lease_seconds":60}`))
	if leaseResponse.Code != http.StatusOK {
		t.Fatalf("lease status=%d body=%s", leaseResponse.Code, leaseResponse.Body.String())
	}
	var lease OperationLease
	if err := json.Unmarshal(leaseResponse.Body.Bytes(), &lease); err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}

	firstBody, _ := json.Marshal(operationProgressRequest{LeaseID: lease.LeaseID, Progress: OperationProgress{Revision: 1, Phase: "running"}})
	first := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-lifecycle-progress-01", http.MethodPost, "/edge/v1/operations/"+operation.ID+"/progress", firstBody)
	if first.Code != http.StatusOK {
		t.Fatalf("progress status=%d body=%s", first.Code, first.Body.String())
	}
	var control OperationControl
	if json.Unmarshal(first.Body.Bytes(), &control) != nil || control.CancelRequested {
		t.Fatalf("control=%+v", control)
	}

	if _, err := store.RequestOperationCancel(operation.ID); err != nil {
		t.Fatal(err)
	}
	secondBody, _ := json.Marshal(operationProgressRequest{LeaseID: lease.LeaseID, Progress: OperationProgress{Revision: 2, Phase: "stopping"}})
	second := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-lifecycle-progress-02", http.MethodPost, "/edge/v1/operations/"+operation.ID+"/progress", secondBody)
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &control) != nil || !control.CancelRequested {
		t.Fatalf("cancel control status=%d body=%s control=%+v", second.Code, second.Body.String(), control)
	}

	cancelBody, _ := json.Marshal(operationCancelRequest{LeaseID: lease.LeaseID})
	cancelledResponse := performSignedRequest(t, handler, device.ID, privateKey, now, "nonce-lifecycle-cancel-01", http.MethodPost, "/edge/v1/operations/"+operation.ID+"/cancel", cancelBody)
	if cancelledResponse.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancelledResponse.Code, cancelledResponse.Body.String())
	}
	var cancelled Operation
	if json.Unmarshal(cancelledResponse.Body.Bytes(), &cancelled) != nil || cancelled.State != OperationCancelled || cancelled.SafeCode != "operation_cancelled" || cancelled.Progress.Revision != 2 {
		t.Fatalf("cancelled=%+v", cancelled)
	}
}
