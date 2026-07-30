package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestInvalidOperationCompletionFailsTerminalInsteadOfRequeueing(t *testing.T) {
	now := time.Date(2026, 7, 30, 17, 30, 0, 0, time.UTC)
	store, err := Open(Config{Root: t.TempDir() + "/edge", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}

	invalid := OperationResult{
		Release: "p15.0.11", Commit: "ca788bbe269f921403fd01b5add1b938f3e0016a",
		ManifestStatus: "valid", ComponentsCompatible: true, ProviderValid: true, DriverValid: true,
		ServiceState: "inactive", ProcessState: "single", LockState: "held", Coherence: "managed",
	}
	completed, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, invalid, "")
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != OperationFailed || completed.SafeCode != "operation_result_invalid" || !emptyOperationResult(completed.Result) {
		t.Fatalf("completed=%+v", completed)
	}
	now = now.Add(MinLeaseTTL + time.Second)
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != "operation_result_invalid" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}
