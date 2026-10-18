package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

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
