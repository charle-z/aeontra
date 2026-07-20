package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestLabOperationIsClosedIdempotentAndDurable(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{Platform: "htb", Machine: "Cap", Target: "10.129.63.164", Difficulty: "easy", OperatingSystem: "linux"}
	created, fresh, err := store.CreateOperation(device.ID, OperationLabPrepare, request)
	if err != nil || !fresh || created.State != OperationQueued {
		t.Fatalf("created=%+v fresh=%t err=%v", created, fresh, err)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationLabPrepare, request)
	if err != nil || fresh || reused.ID != created.ID {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != created.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{WorkspaceID: "ws_0123456789abcdef0123456789abcdef", AuthorizationRevision: 1}
	completed, err := store.CompleteOperation(device.ID, created.ID, lease.LeaseID, result, "")
	if err != nil || completed.State != OperationSucceeded || completed.Result != result {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	after, err := store.OperationStatus(created.ID)
	if err != nil || after.Result != result {
		t.Fatalf("after=%+v err=%v", after, err)
	}
}

func TestLabOperationsRejectPublicTargetsAndUnsafeCompletion(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	if _, _, err := store.CreateOperation(device.ID, OperationLabPrepare, OperationRequest{Platform: "htb", Machine: "Cap", Target: "8.8.8.8", Difficulty: "easy", OperatingSystem: "linux"}); err == nil {
		t.Fatal("public target accepted")
	}
	if validOperationCompletion(OperationResult{}, "contains-target-10.10.10.10") {
		t.Fatal("unsafe failure code accepted")
	}
}
