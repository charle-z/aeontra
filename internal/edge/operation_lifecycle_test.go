package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationLifecyclePersistsProgressListsActiveAndCancels(t *testing.T) {
	now := time.Date(2026, 7, 29, 20, 45, 0, 0, time.UTC)
	root := filepath.Join(t.TempDir(), "edge")
	store, err := Open(Config{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	operation, fresh, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil || !fresh {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	control, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 1, Phase: "running", CompletedUnits: 1, TotalUnits: 3})
	if err != nil || control.CancelRequested {
		t.Fatalf("control=%+v err=%v", control, err)
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 1 || active[0].ID != operation.ID || active[0].Progress.Revision != 1 || active[0].Progress.Phase != "running" {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	cancelling, err := store.RequestOperationCancel(operation.ID)
	if err != nil || !cancelling.CancelRequested || cancelling.State != OperationLeased {
		t.Fatalf("cancelling=%+v err=%v", cancelling, err)
	}
	if _, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, OperationResult{}, "operation_failed"); err == nil {
		t.Fatal("cancel-requested operation completed")
	}
	control, err = store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 2, Phase: "stopping", CompletedUnits: 2, TotalUnits: 3})
	if err != nil || !control.CancelRequested {
		t.Fatalf("control after cancel=%+v err=%v", control, err)
	}
	cancelled, err := store.CancelLeasedOperation(device.ID, operation.ID, lease.LeaseID)
	if err != nil || cancelled.State != OperationCancelled || !cancelled.CancelRequested || cancelled.SafeCode != "operation_cancelled" || cancelled.Progress.Revision != 2 {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	active, err = store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active after cancel=%+v err=%v", active, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Config{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	persisted, err := reopened.OperationStatus(operation.ID)
	if err != nil || persisted.State != OperationCancelled || persisted.Progress.Revision != 2 || persisted.Progress.Phase != "stopping" {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}

func TestExpiredOperationLeaseRecoversQueuedOrFinishesCancellation(t *testing.T) {
	now := time.Date(2026, 7, 29, 20, 45, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	first, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	second, _, _ := store.CreateOperation(device.ID, OperationOnboardingStatus, OperationRequest{})
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || (lease.Operation.ID != first.ID && lease.Operation.ID != second.ID) {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	otherID := first.ID
	if lease.Operation.ID == first.ID {
		otherID = second.ID
	}
	if _, err := store.RequestOperationCancel(lease.Operation.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL + time.Second)
	next, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || next.Operation.ID != otherID {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	cancelled, err := store.OperationStatus(lease.Operation.ID)
	if err != nil || cancelled.State != OperationCancelled || cancelled.SafeCode != "operation_cancelled" {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
}

func TestOperationProgressRejectsReplayAndUnsafeBounds(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	lease, _ := store.LeaseOperation(device.ID, time.Minute)
	valid := OperationProgress{Revision: 1, Phase: "running", CompletedUnits: 1, TotalUnits: 2}
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []OperationProgress{
		valid,
		{Revision: 2, Phase: "contains target 10.0.0.1", CompletedUnits: 1, TotalUnits: 2},
		{Revision: 2, Phase: "running", CompletedUnits: 3, TotalUnits: 2},
		{Revision: 2, Phase: "running", TotalUnits: MaxOperationProgressUnits + 1},
	} {
		if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, invalid); err == nil {
			t.Fatalf("unsafe progress accepted: %+v", invalid)
		}
	}
}
