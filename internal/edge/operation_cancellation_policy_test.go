package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestBundleEffectsBecomeNonCancellableAfterPickup(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	operation, _, err := store.CreateOperation(device.ID, OperationBundleUpdate, OperationRequest{Release: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if !OperationCanCancel(operation) {
		t.Fatal("queued bundle update should be cancellable before pickup")
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if OperationCanCancel(lease.Operation) {
		t.Fatal("leased bundle update reported cancellable")
	}
	if _, err := store.RequestOperationCancel(operation.ID); err == nil {
		t.Fatal("running bundle update accepted cancellation")
	}
	status, err := store.OperationStatus(operation.ID)
	if err != nil || status.CancelRequested || status.State != OperationLeased {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
