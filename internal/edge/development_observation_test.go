package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentProcessObservationConsumesCompletedJournalWithoutCreating(t *testing.T) {
	store := openHTTPTestStore(t)
	fixedNow := time.Now().UTC()
	store.now = func() time.Time { return fixedNow }
	code, _ := store.CreatePairing(time.Minute)
	key, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", key)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", BackgroundProcessID: "pr_" + strings.Repeat("a", 32), OutputLimit: 8192}
	if _, found, err := store.LatestDevelopmentProcessOperation(device.ID, OperationProjectProcessStatus, request); err != nil || found {
		t.Fatalf("absent observation: found=%v err=%v", found, err)
	}
	op, _, err := store.CreateOperation(device.ID, OperationProjectProcessStatus, request)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != op.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	result := OperationResult{WorkspaceID: "ws_" + strings.Repeat("b", 32), ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "project", ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev", BackgroundProcessID: request.BackgroundProcessID, BackgroundProcessState: "exited", BackgroundStartedAt: started.Format(time.RFC3339Nano), BackgroundFinishedAt: started.Add(time.Second).Format(time.RFC3339Nano), BackgroundExitKnown: true, BackgroundStdoutEOF: true, BackgroundStderrEOF: true}
	if _, err := store.CompleteOperation(device.ID, op.ID, lease.LeaseID, result, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		observed, found, err := store.LatestDevelopmentProcessOperation(device.ID, OperationProjectProcessStatus, request)
		if err != nil || !found || observed.ID != op.ID || observed.State != OperationSucceeded || !observed.Result.BackgroundExitKnown {
			t.Fatalf("lost terminal observation: %+v found=%v err=%v", observed, found, err)
		}
	}
	other := request
	other.BackgroundProcessID = "pr_" + strings.Repeat("c", 32)
	if _, found, err := store.LatestDevelopmentProcessOperation(device.ID, OperationProjectProcessStatus, other); err != nil || found {
		t.Fatal("lookup crossed process identity")
	}
	if _, _, err := store.LatestDevelopmentProcessOperation(device.ID, OperationProjectProcessStart, request); err == nil {
		t.Fatal("lookup acquired start authority")
	}
	next, _, err := store.CreateOperation(device.ID, OperationProjectProcessStatus, request)
	if err != nil || next.ID == op.ID {
		t.Fatalf("new observation: %+v err=%v", next, err)
	}
	observed, found, err := store.LatestDevelopmentProcessOperation(device.ID, OperationProjectProcessStatus, request)
	if err != nil || !found || observed.ID != next.ID || observed.State != OperationQueued {
		t.Fatalf("lookup lost latest observation at same timestamp: %+v err=%v", observed, err)
	}
}
