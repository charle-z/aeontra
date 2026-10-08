package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueuedProjectExecExpiresBeforeOfflineEdgeCanRunIt(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	store, err := Open(Config{Root: filepath.Join(t.TempDir(), "edge"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	exec, _, err := store.CreateOperation(device.ID, OperationProjectExec, OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell",
		IdempotencyKey: "stale-interactive-exec", Argv: []string{"touch", "should-not-exist"}, TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(3*time.Minute + time.Second)
	status, err := store.OperationLifecycleStatus(exec.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != "operation_queue_expired" {
		t.Fatalf("stale status=%+v err=%v", status, err)
	}
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); !errors.Is(err, ErrNoTaskAvailable) {
		t.Fatalf("stale interactive command became executable: %v", err)
	}
	active, err := store.ActiveOperations(device.ID, 10)
	if err != nil || len(active) != 0 {
		t.Fatalf("active=%+v err=%v", active, err)
	}
	late, _, err := store.CreateOperation(device.ID, OperationProjectExec, OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell",
		IdempotencyKey: "stale-at-lease", Argv: []string{"true"}, TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(projectExecQueueTTL + time.Second)
	if _, err := store.LeaseOperation(device.ID, MinLeaseTTL); !errors.Is(err, ErrNoTaskAvailable) {
		t.Fatalf("stale command leased on Edge reconnect: %v", err)
	}
	status, err = store.OperationStatus(late.ID)
	if err != nil || status.State != OperationFailed || status.SafeCode != projectExecQueueExpired {
		t.Fatalf("late status=%+v err=%v", status, err)
	}
	durable, _, err := store.CreateOperation(device.ID, OperationBundleStatus, OperationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(projectExecQueueTTL + time.Second)
	lease, err := store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || lease.Operation.ID != durable.ID {
		t.Fatalf("unrelated durable operation was restricted: lease=%+v err=%v", lease, err)
	}
	onTime, _, err := store.CreateOperation(device.ID, OperationProjectExec, OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell",
		IdempotencyKey: "within-interactive-wait", Argv: []string{"true"}, TimeoutSeconds: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(projectExecQueueTTL - time.Second)
	lease, err = store.LeaseOperation(device.ID, MinLeaseTTL)
	if err != nil || lease.Operation.ID != onTime.ID {
		t.Fatalf("fresh interactive command was restricted: lease=%+v err=%v", lease, err)
	}
}

func TestProjectExecOperationIsDurablyIdempotentAndBounded(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{
		Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell",
		IdempotencyKey: "chat-exec-1",
		Argv:           []string{"go", "test", "./..."}, CWD: "./internal",
		Stdin: "input\n", Environment: map[string]string{"CI": "true"}, TimeoutSeconds: 90,
	}
	operation, fresh, err := store.CreateOperation(device.ID, OperationProjectExec, request)
	if err != nil || !fresh || operation.State != OperationQueued {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	if operation.Request.Alias != "project" || operation.Request.TargetAlias != "parrot" || operation.Request.CWD != "internal" {
		t.Fatalf("normalized request=%+v", operation.Request)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationProjectExec, request)
	if err != nil || fresh || reused.ID != operation.ID {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	conflict := request
	conflict.Environment = map[string]string{"CI": "false"}
	if _, _, err := store.CreateOperation(device.ID, OperationProjectExec, conflict); err == nil {
		t.Fatal("idempotency key accepted different execution parameters")
	}
	lease, err := store.LeaseOperation(device.ID, time.Minute)
	if err != nil || lease.Operation.ID != operation.ID {
		t.Fatalf("lease=%+v err=%v", lease, err)
	}
	result := OperationResult{
		WorkspaceID:  "ws_0123456789abcdef0123456789abcdef",
		ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		ExecCompleted: true, ExecExitCode: 0, ExecStdout: "ok\n",
	}
	completed, err := store.CompleteOperation(device.ID, operation.ID, lease.LeaseID, result, "")
	if err != nil || completed.State != OperationSucceeded {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	terminal, fresh, err := store.CreateOperation(device.ID, OperationProjectExec, request)
	if err != nil || fresh || terminal.ID != operation.ID || terminal.State != OperationSucceeded {
		t.Fatalf("terminal=%+v fresh=%t err=%v", terminal, fresh, err)
	}
	for _, invalid := range []OperationRequest{
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bad-1", TimeoutSeconds: 10},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bad-2", Argv: []string{"pwd"}, CWD: "../outside", TimeoutSeconds: 10},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bad-3", Argv: []string{"pwd"}, TimeoutSeconds: 121},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bad-4", Argv: []string{"pwd"}, Environment: map[string]string{"PATH": "/tmp"}, TimeoutSeconds: 10},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "bad-5", Argv: []string{"cat"}, Stdin: strings.Repeat("x", MaxProjectExecStdinBytes+1), TimeoutSeconds: 10},
	} {
		if _, _, err := store.CreateOperation(device.ID, OperationProjectExec, invalid); err == nil {
			t.Fatalf("unsafe project execution request accepted: %+v", invalid)
		}
	}
	unsafe := result
	unsafe.ExecStdout = strings.Repeat("x", MaxProjectExecStreamBytes+1)
	if validOperationCompletion(unsafe, "") {
		t.Fatal("oversized project execution result accepted")
	}
}
