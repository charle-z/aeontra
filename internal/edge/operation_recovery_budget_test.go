package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

func TestPrivilegedOperationRecoveryBudgetFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 30, 16, 30, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, fresh, err := store.CreateOperation(device.ID, OperationBundleUpdate, OperationRequest{Release: "stable"})
	if err != nil || !fresh {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}

	for attempt := 1; attempt <= maxOperationLeaseAttempts; attempt++ {
		lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
		if err != nil || lease.Operation.ID != operation.ID {
			t.Fatalf("attempt=%d lease=%+v err=%v", attempt, lease, err)
		}
		now = now.Add(MinLeaseTTL + time.Second)
		status, err := store.OperationLifecycleStatus(operation.ID)
		if err != nil {
			t.Fatalf("attempt=%d status error=%v", attempt, err)
		}
		if attempt < maxOperationLeaseAttempts {
			if status.State != OperationQueued || status.SafeCode != "" {
				t.Fatalf("attempt=%d status=%+v", attempt, status)
			}
			continue
		}
		if status.State != OperationFailed || status.SafeCode != privilegedOperationRecoveryExhaustedCode || status.CancelRequested {
			t.Fatalf("exhausted status=%+v", status)
		}
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
}

func TestPrivilegedOperationRecoveryWindowFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 30, 16, 35, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationEdgeRepair, OperationRequest{})
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	now = now.Add(maxPrivilegedOperationRecoveryWindow + time.Second)
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != privilegedOperationRecoveryExhaustedCode {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestNormalOperationRecoveryBudgetFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 30, 16, 40, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	for attempt := 1; attempt <= maxOperationLeaseAttempts; attempt++ {
		lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
		if err != nil || lease.Operation.ID != operation.ID {
			t.Fatalf("attempt=%d lease=%+v err=%v", attempt, lease, err)
		}
		now = now.Add(MinLeaseTTL + time.Second)
		if attempt < maxOperationLeaseAttempts {
			status, err := store.OperationLifecycleStatus(operation.ID)
			if err != nil || status.State != OperationQueued || status.SafeCode != "" {
				t.Fatalf("attempt=%d status=%+v err=%v", attempt, status, err)
			}
		}
	}
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != privilegedOperationRecoveryExhaustedCode {
		t.Fatalf("exhausted status=%+v err=%v", status, err)
	}
}

func TestStartedNormalOperationExpiresAsInterruptedWithoutReplay(t *testing.T) {
	now := time.Date(2026, 9, 4, 1, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, err := store.CreateOperation(device.ID, OperationProjectExec, OperationRequest{
		Alias:          "repo",
		TargetAlias:    "parrot",
		Profile:        "linux-workcell",
		IdempotencyKey: "interrupted-project-exec",
		Argv:           []string{"true"},
		TimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 1, Phase: "running"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(operationHeartbeatTTL + time.Second)
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != operationExecutionInterruptedCode || status.Progress.Revision != 1 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); err == nil {
		t.Fatal("interrupted operation was leased for duplicate execution")
	}
}

func TestStartedBundleOperationRetainsReceiptRecoveryRoute(t *testing.T) {
	now := time.Date(2026, 9, 4, 1, 5, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, err := store.CreateOperation(device.ID, OperationBundleUpdate, OperationRequest{Release: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 1, Phase: "updating"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(operationHeartbeatTTL + time.Second)
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationQueued || status.SafeCode != "" || status.Progress.Revision != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
