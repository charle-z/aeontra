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

func TestAutopilotControlOperationsPublishOnlySafeDurableMetadata(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	workspaceID := "ws_0123456789abcdef0123456789abcdef"
	if _, err := store.RegisterWorkspaces(device.ID, []WorkspaceRegistration{{WorkspaceID: workspaceID, Profile: "linux-workcell", Mode: "htb-linux"}}); err != nil {
		t.Fatal(err)
	}
	operation, _, err := store.CreateOperation(device.ID, OperationAutopilotStart, OperationRequest{WorkspaceID: workspaceID, RunUntil: "completed_or_cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{WorkspaceID: workspaceID, JobID: "aj_0123456789abcdef0123456789abcdef", JobState: "running", ProgressRevision: 3, CycleCount: 7}
	if _, err = store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.ReportAutopilot(device.ID, result); err != nil {
		t.Fatal(err)
	}
	status, err := store.AutopilotStatus(workspaceID)
	if err != nil || status != result {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	second, fresh, err := store.CreateOperation(device.ID, OperationAutopilotStart, OperationRequest{WorkspaceID: workspaceID, RunUntil: "completed_or_cancelled"})
	if err != nil || !fresh || second.ID == operation.ID {
		t.Fatalf("second=%+v fresh=%t err=%v", second, fresh, err)
	}
}
