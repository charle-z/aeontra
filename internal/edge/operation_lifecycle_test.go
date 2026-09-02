package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"strings"
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

func TestExpiredOperationLeaseReturnsBehindQueuedOperation(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 20, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	front, _, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	behind, _, err := store.CreateOperation(device.ID, OperationOnboardingStatus, OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL + time.Second)
	next, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if next.Operation.ID != behind.ID {
		t.Fatalf("expired operation %s was selected before queued operation %s", front.ID, behind.ID)
	}
}

func TestExpiredOperationLeaseRecoversOnReadAndIdempotentReuse(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 10, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, fresh, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil || !fresh {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 4, Phase: "running", CompletedUnits: 1, TotalUnits: 2}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(operationHeartbeatTTL + time.Second)

	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationQueued || status.Progress.Revision != 0 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	legacy, err := store.OperationStatus(operation.ID)
	if err != nil || legacy.State != OperationQueued {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 1 || active[0].ID != operation.ID || active[0].State != OperationQueued || active[0].Progress.Revision != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil || fresh || reused.ID != operation.ID || reused.State != OperationQueued {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	retry, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || retry.Operation.ID != operation.ID {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, retry.LeaseID, OperationProgress{Revision: 1, Phase: "running", CompletedUnits: 1, TotalUnits: 2}); err != nil {
		t.Fatalf("fresh progress after recovery failed: %v", err)
	}
}

func TestExpiredOperationLeaseRecoversBeforeIdempotentReuse(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 11, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL)

	reused, fresh, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil || fresh || reused.ID != operation.ID || reused.State != OperationQueued {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
}

func TestExpiredOperationLeaseCanBeCancelledAfterExpiry(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 11, 30, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL)

	cancelled, err := store.RequestOperationCancel(operation.ID)
	if err != nil || cancelled.State != OperationCancelled || !cancelled.CancelRequested || cancelled.SafeCode != "operation_cancelled" {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
}

func TestExpiredCancelledOperationLeaseClosesOnRead(t *testing.T) {
	now := time.Date(2026, 7, 30, 0, 12, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, _ := store.Pair(code, "parrot-edge", publicKey)
	operation, _, _ := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestOperationCancel(operation.ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(MinLeaseTTL + time.Second)

	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.State != OperationCancelled || !status.CancelRequested || status.SafeCode != "operation_cancelled" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
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

func TestOperationLifecyclePersistsAuthoritativeTiming(t *testing.T) {
	now := time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC)
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
	now = now.Add(1250 * time.Millisecond)
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Millisecond)
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 1, Phase: "running"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(40 * time.Millisecond)
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 2, Phase: "finalizing"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(15 * time.Millisecond)
	commit := strings.Repeat("0", 40)
	result := OperationResult{
		Release: "p15.0.24", Commit: commit, ManifestStatus: "valid", ComponentsCompatible: true,
		ServiceActive: true, ServiceState: "active", ProcessState: "single", LockState: "held", Coherence: "managed",
		ProcessRelease: "p15.0.24", ProcessCommit: commit, Paired: true, BubblewrapValid: true, RootlessValid: true,
		ProviderValid: true, DriverValid: true,
	}
	completed, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, "")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC)
	if !completed.LeasedAt.Equal(created.Add(1250*time.Millisecond)) ||
		!completed.RunningAt.Equal(created.Add(1275*time.Millisecond)) ||
		!completed.FinalizingAt.Equal(created.Add(1315*time.Millisecond)) ||
		!completed.UpdatedAt.Equal(created.Add(1330*time.Millisecond)) {
		t.Fatalf("unexpected timing: %+v", completed)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Config{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.OperationLifecycleStatus(operation.ID)
	if err != nil || !persisted.LeasedAt.Equal(completed.LeasedAt) || !persisted.RunningAt.Equal(completed.RunningAt) || !persisted.FinalizingAt.Equal(completed.FinalizingAt) {
		t.Fatalf("persisted=%+v completed=%+v err=%v", persisted, completed, err)
	}
}

func TestOperationLifecycleRejectsInvalidTimingOrder(t *testing.T) {
	operation := Operation{
		CreatedAt:    time.Date(2026, 8, 3, 23, 0, 0, 0, time.UTC),
		LeasedAt:     time.Date(2026, 8, 3, 23, 0, 2, 0, time.UTC),
		RunningAt:    time.Date(2026, 8, 3, 23, 0, 1, 0, time.UTC),
		FinalizingAt: time.Date(2026, 8, 3, 23, 0, 3, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 8, 3, 23, 0, 4, 0, time.UTC),
	}
	if validOperationTiming(operation) {
		t.Fatalf("invalid timing order accepted: %+v", operation)
	}
	operation.RunningAt = operation.LeasedAt
	if !validOperationTiming(operation) {
		t.Fatalf("valid timing order rejected: %+v", operation)
	}
	operation.LeasedAt = time.Time{}
	if validOperationTiming(operation) {
		t.Fatalf("partial timing accepted: %+v", operation)
	}
}

func TestOperationTimingIgnoresOutOfOrderPhases(t *testing.T) {
	now := time.Date(2026, 8, 4, 1, 0, 0, 0, time.UTC)
	store := openOperationTimingTestStore(t, &now)
	device := pairOperationTimingTestDevice(t, store)
	operation, fresh, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil || !fresh {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	now = now.Add(time.Second)
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Millisecond)
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 1, Phase: "finalizing"}); err != nil {
		t.Fatal(err)
	}
	status, err := store.OperationLifecycleStatus(operation.ID)
	if err != nil || !status.RunningAt.IsZero() || !status.FinalizingAt.IsZero() {
		t.Fatalf("out-of-order finalizing was recorded: %+v err=%v", status, err)
	}
	now = now.Add(10 * time.Millisecond)
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 2, Phase: "running"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Millisecond)
	if _, err := store.ReportOperationProgress(device.ID, operation.ID, lease.LeaseID, OperationProgress{Revision: 3, Phase: "finalizing"}); err != nil {
		t.Fatal(err)
	}
	status, err = store.OperationLifecycleStatus(operation.ID)
	if err != nil || status.RunningAt.IsZero() || status.FinalizingAt.IsZero() || status.FinalizingAt.Before(status.RunningAt) {
		t.Fatalf("ordered timing was not recorded: %+v err=%v", status, err)
	}
}

func openOperationTimingTestStore(t *testing.T, now *time.Time) *Store {
	t.Helper()
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func pairOperationTimingTestDevice(t *testing.T, store *Store) Device {
	t.Helper()
	code, err := store.CreatePairing(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device, err := store.Pair(code, "parrot-edge", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return device
}
