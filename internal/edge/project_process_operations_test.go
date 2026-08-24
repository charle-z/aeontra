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
	stdin := OperationRequest{
		Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-1",
		BackgroundProcessID: "pr_0123456789abcdef0123456789abcdef", Stdin: "hello\n",
	}
	stdinOperation, fresh, err := store.CreateOperation(device.ID, OperationProjectProcessStdin, stdin)
	if err != nil || !fresh {
		t.Fatalf("stdin operation=%+v fresh=%t err=%v", stdinOperation, fresh, err)
	}
	stdinReused, fresh, err := store.CreateOperation(device.ID, OperationProjectProcessStdin, stdin)
	if err != nil || fresh || stdinReused.ID != stdinOperation.ID {
		t.Fatalf("stdin reused=%+v fresh=%t err=%v", stdinReused, fresh, err)
	}
	stdinConflict := stdin
	stdinConflict.Stdin = "other\n"
	if _, _, err := store.CreateOperation(device.ID, OperationProjectProcessStdin, stdinConflict); err == nil {
		t.Fatal("idempotency key accepted different stdin content")
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
	stdin := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-1", BackgroundProcessID: status.BackgroundProcessID, Stdin: "{\"jsonrpc\":\"2.0\"}\n"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStdin, stdin); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []OperationRequest{
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-empty", BackgroundProcessID: status.BackgroundProcessID},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-negative", BackgroundProcessID: status.BackgroundProcessID, Stdin: "x", ProcessStdinOffset: -1},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-overflow", BackgroundProcessID: status.BackgroundProcessID, Stdin: "xx", ProcessStdinOffset: MaxProjectProcessStdinTotalBytes - 1},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-large", BackgroundProcessID: status.BackgroundProcessID, Stdin: strings.Repeat("x", MaxProjectProcessStdinBytes+1)},
		{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "stdin-secret", BackgroundProcessID: status.BackgroundProcessID, Stdin: "ghp_abcdefghijklmnopqrstuvwxyz0123456789AB"},
	} {
		if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStdin, invalid); err == nil {
			t.Fatalf("unsafe stdin request accepted: %+v", invalid)
		}
	}
	stop := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", BackgroundProcessID: status.BackgroundProcessID, GraceSeconds: 5}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessStop, stop); err != nil {
		t.Fatal(err)
	}
	signal := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", BackgroundProcessID: status.BackgroundProcessID, BackgroundSignal: "interrupt"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessSignal, signal); err != nil {
		t.Fatal(err)
	}
	signal.BackgroundSignal = "19"
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessSignal, signal); err == nil {
		t.Fatal("arbitrary process signal accepted")
	}
	list := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", ProcessLimit: 100}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessList, list); err != nil {
		t.Fatal(err)
	}
	cleanup := OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell"}
	if _, err := validateOperationRequestWithProjectExec(OperationProjectProcessCleanup, cleanup); err != nil {
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
	stdinResult := result
	stdinResult.BackgroundStdout = ""
	stdinResult.BackgroundStdoutNext = 0
	stdinResult.BackgroundStdinNext = int64(len(stdin.Stdin))
	stdinResult.BackgroundStdinAccepted = len(stdin.Stdin)
	if !validOperationCompletionForKind(OperationProjectProcessStdin, stdinResult, "") {
		t.Fatalf("valid stdin result rejected: %+v", stdinResult)
	}
	closedResult := stdinResult
	closedResult.BackgroundStdinNext = 0
	closedResult.BackgroundStdinAccepted = 0
	closedResult.BackgroundStdinClosed = true
	if !validOperationCompletionForKind(OperationProjectProcessStdin, closedResult, "") {
		t.Fatalf("valid close-only stdin result rejected: %+v", closedResult)
	}
	partialResult := stdinResult
	partialResult.BackgroundStdinNext = 4
	partialResult.BackgroundStdinAccepted = 4
	partialResult.BackgroundStdinClosed = true
	if !validOperationCompletionForKind(OperationProjectProcessStdin, partialResult, "") {
		t.Fatalf("valid partial closed stdin result rejected: %+v", partialResult)
	}
	partialResult.BackgroundStdinNext = 3
	if validOperationCompletionForKind(OperationProjectProcessStdin, partialResult, "") {
		t.Fatal("stdin result accepted next offset below accepted bytes")
	}
	if validOperationCompletionForKind(OperationProjectProcessStatus, stdinResult, "") {
		t.Fatal("stdin receipt accepted for process status")
	}
	if validOperationCompletionForKind(OperationProjectExec, result, "") {
		t.Fatal("project process result accepted for foreground execution")
	}
	base := OperationResult{
		WorkspaceID: "ws_0123456789abcdef0123456789abcdef", ProjectAlias: "project", ProjectOwner: "charle-z", ProjectRepository: "repo",
		ProjectTarget: "parrot", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
	}
	listResult := base
	listResult.BackgroundProcesses = []BackgroundProcessSummary{{ProcessID: status.BackgroundProcessID, State: "running", StartedAt: time.Unix(1700000000, 0).UTC().Format(time.RFC3339Nano)}}
	if !validOperationCompletionForKind(OperationProjectProcessList, listResult, "") || validOperationCompletionForKind(OperationProjectProcessCleanup, listResult, "") {
		t.Fatal("bounded process list result kind validation failed")
	}
	cleanupResult := base
	cleanupResult.BackgroundCleanupRemoved = 1
	cleanupResult.BackgroundCleanupActive = 2
	if !validOperationCompletionForKind(OperationProjectProcessCleanup, cleanupResult, "") || validOperationCompletionForKind(OperationProjectProcessList, cleanupResult, "") {
		t.Fatal("process cleanup result kind validation failed")
	}
	result.BackgroundStdout = strings.Repeat("x", MaxProjectProcessReadBytes+1)
	if validOperationCompletion(result, "") {
		t.Fatal("oversized process output accepted")
	}
}
