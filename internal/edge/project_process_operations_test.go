package edge

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func TestProjectProcessStartIsDurablyIdempotentAndConflictsSafely(t *testing.T) {
	store := openHTTPTestStore(t)
	code, _ := store.CreatePairing(time.Minute)
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	device, err := store.Pair(code, "parrot", publicKey)
	if err != nil {
		t.Fatal(err)
	}
	request := OperationRequest{
		Alias: "Project", TargetAlias: "Parrot", Profile: "linux-workcell",
		IdempotencyKey: "chat-process-1", Argv: []string{"go", "run", "./cmd/server"},
		CWD: "cmd", Stdin: "ready\n", Environment: map[string]string{"PORT": "8080"},
	}
	operation, fresh, err := store.CreateOperation(device.ID, OperationProjectProcessStart, request)
	if err != nil || !fresh || operation.State != OperationQueued {
		t.Fatalf("operation=%+v fresh=%t err=%v", operation, fresh, err)
	}
	reused, fresh, err := store.CreateOperation(device.ID, OperationProjectProcessStart, request)
	if err != nil || fresh || reused.ID != operation.ID {
		t.Fatalf("reused=%+v fresh=%t err=%v", reused, fresh, err)
	}
	conflict := request
	conflict.Argv = []string{"go", "run", "./cmd/other"}
	if _, _, err := store.CreateOperation(device.ID, OperationProjectProcessStart, conflict); err == nil {
		t.Fatal("idempotency key accepted different background process parameters")
	}
}

func TestProjectProcessRequestsAndResultsAreClosedAndBounded(t *testing.T) {
	validStart := OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "process-1",
		Argv: []string{"go", "run", "."}, Environment: map[string]string{"PORT": "8080"},
	}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStart, validStart); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []OperationRequest{
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "process-2", Argv: []string{"sh", "-c", "echo token=ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"}},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "process-3", Argv: []string{"cat"}, Stdin: "password=thisisarealsecretvalue"},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "process-4", Argv: []string{"env"}, Environment: map[string]string{"API_TOKEN": "thisisarealsecretvalue"}},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "process-5", Argv: []string{"pwd"}, CWD: "../outside"},
	} {
		if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStart, invalid); err == nil {
			t.Fatalf("unsafe process start accepted: %+v", invalid)
		}
	}
	status := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", BackgroundProcessID: "pr_0123456789abcdef0123456789abcdef", StdoutOffset: 10, StderrOffset: 20, OutputLimit: 4096}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStatus, status); err != nil {
		t.Fatal(err)
	}
	stop := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", BackgroundProcessID: status.BackgroundProcessID, GraceSeconds: 5}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStop, stop); err != nil {
		t.Fatal(err)
	}
	result := OperationResult{
		WorkspaceID: "ws_0123456789abcdef0123456789abcdef", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		BackgroundProcessID: status.BackgroundProcessID, BackgroundProcessState: "running", BackgroundStartedAt: time.Unix(1700000000, 0).UTC().Format(time.RFC3339Nano),
		BackgroundStdout: "ready\n", BackgroundStdoutNext: 6,
	}
	if !validOperationCompletion(result, "") {
		t.Fatalf("valid process result rejected: %+v", result)
	}
	if validOperationCompletionForKind(OperationProjectExec, result, "") {
		t.Fatal("project process result accepted for foreground execution")
	}
	result.BackgroundStdout = strings.Repeat("x", MaxProjectProcessReadBytes+1)
	if validOperationCompletion(result, "") {
		t.Fatal("oversized process output accepted")
	}
}
