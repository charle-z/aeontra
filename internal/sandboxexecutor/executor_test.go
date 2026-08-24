package sandboxexecutor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/sandboxprotocol"
)

const executorTestDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type fakeEngine struct {
	mu       sync.Mutex
	attest   error
	runs     int
	lastSpec RunSpec
	response sandboxprotocol.Response
	err      error
	started  chan struct{}
	release  chan struct{}
}

func (f *fakeEngine) Attest(context.Context, string, string) error { return f.attest }
func (f *fakeEngine) Run(_ context.Context, spec RunSpec) (sandboxprotocol.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs++
	f.lastSpec = spec
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		<-f.release
	}
	return f.response, f.err
}

func testExecutor(t *testing.T, engine Engine) *Executor {
	t.Helper()
	root := t.TempDir()
	executor, err := New(Config{
		Token: strings.Repeat("t", 32), WorkspaceID: "primary", WorkspaceRoot: root, StateRoot: t.TempDir(),
		Image: "localhost/aeontra-l3@" + executorTestDigest, ImageDigest: executorTestDigest,
		MaxTimeoutMS: 120000, MaxCPUMillis: 1000, MaxMemoryMiB: 1024,
		MaxProcessLimit: 256, MaxOutputBytes: 1 << 20, MaxConcurrent: 2, Engine: engine,
	})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func validRequest(t *testing.T, executor *Executor) sandboxprotocol.Request {
	t.Helper()
	request := sandboxprotocol.Request{
		SchemaVersion: 1, IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
		WorkspaceID: "primary", RelativeDir: "", Argv: []string{"bash", "-lc", "cargo test"},
		NetworkProfile: "none", TimeoutMS: 30000, CPUMillis: 1000, MemoryMiB: 1024,
		ProcessLimit: 256, OutputBytes: 1 << 20,
	}
	var err error
	request.RequestDigest, err = sandboxprotocol.Digest(request)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestStatusFailsClosedOnAttestationDrift(t *testing.T) {
	executor := testExecutor(t, &fakeEngine{attest: errors.New("not rootless")})
	status := executor.Status(context.Background())
	if status.Available || status.Rootless {
		t.Fatalf("drifting engine reported available: %#v", status)
	}
}

func TestNewRejectsStateInsideWritableWorkspace(t *testing.T) {
	root := t.TempDir()
	_, err := New(Config{
		Token: strings.Repeat("t", 32), WorkspaceID: "primary", WorkspaceRoot: root,
		StateRoot: filepath.Join(root, ".state"), Image: "image@" + executorTestDigest,
		ImageDigest: executorTestDigest, MaxTimeoutMS: 1000, MaxCPUMillis: 1000,
		MaxMemoryMiB: 128, MaxProcessLimit: 16, MaxOutputBytes: 1024, Engine: &fakeEngine{},
		MaxConcurrent: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("writable workspace could modify executor receipts: %v", err)
	}
}

func TestExecuteUsesServerOwnedWorkspaceAndBounds(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0, Stdout: "ok"}}
	executor := testExecutor(t, engine)
	request := validRequest(t, executor)
	response, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Stdout != "ok" || engine.runs != 1 {
		t.Fatalf("response/runs = %#v/%d", response, engine.runs)
	}
	if engine.lastSpec.WorkspaceRoot != executor.config.WorkspaceRoot || engine.lastSpec.NetworkProfile != "none" {
		t.Fatalf("server-owned spec mismatch: %#v", engine.lastSpec)
	}
	if engine.lastSpec.Image != executor.config.Image || engine.lastSpec.OutputBytes != request.OutputBytes {
		t.Fatalf("caller influenced image or bounds: %#v", engine.lastSpec)
	}
}

func TestExecuteReplaysCompletedReceiptWithoutRepeatingEffect(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0, Stdout: "once"}}
	executor := testExecutor(t, engine)
	request := validRequest(t, executor)
	first, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if engine.runs != 1 || first != second {
		t.Fatalf("effect replayed or result changed: runs=%d first=%#v second=%#v", engine.runs, first, second)
	}
}

func TestExecuteConcurrentSameIdentityNeverRepeatsEffect(t *testing.T) {
	engine := &fakeEngine{
		response: sandboxprotocol.Response{ExitCode: 0, Stdout: "once"},
		started:  make(chan struct{}, 1), release: make(chan struct{}),
	}
	executor := testExecutor(t, engine)
	request := validRequest(t, executor)
	type outcome struct {
		response sandboxprotocol.Response
		err      error
	}
	firstResult := make(chan outcome, 1)
	secondResult := make(chan outcome, 1)
	go func() {
		response, err := executor.Execute(context.Background(), request)
		firstResult <- outcome{response: response, err: err}
	}()
	<-engine.started
	go func() {
		response, err := executor.Execute(context.Background(), request)
		secondResult <- outcome{response: response, err: err}
	}()
	close(engine.release)
	first := <-firstResult
	second := <-secondResult
	if first.err != nil {
		t.Fatalf("first execution failed: %#v", first)
	}
	if second.err == nil && first.response != second.response {
		t.Fatalf("successful concurrent replay changed result: first=%#v second=%#v", first, second)
	}
	if second.err != nil && !strings.Contains(second.err.Error(), "indeterminate") {
		t.Fatalf("concurrent retry did not fail closed: %v", second.err)
	}
	engine.mu.Lock()
	runs := engine.runs
	engine.mu.Unlock()
	if runs != 1 {
		t.Fatalf("same identity executed %d times", runs)
	}
}

func TestExecuteRejectsSecretWorkspaceBeforeEngine(t *testing.T) {
	engine := &fakeEngine{}
	executor := testExecutor(t, engine)
	if err := os.WriteFile(filepath.Join(executor.config.WorkspaceRoot, ".env"), []byte("TOKEN=value"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := executor.Execute(context.Background(), validRequest(t, executor))
	if err == nil || !strings.Contains(err.Error(), "secret") || engine.runs != 0 {
		t.Fatalf("secret workspace was not rejected before engine: err=%v runs=%d", err, engine.runs)
	}
}

func TestExecuteRejectsTraversalEnvironmentAndResourceEscalation(t *testing.T) {
	for name, mutate := range map[string]func(*sandboxprotocol.Request){
		"traversal":          func(r *sandboxprotocol.Request) { r.RelativeDir = "../outside" },
		"secret environment": func(r *sandboxprotocol.Request) { r.Environment = map[string]string{"GITHUB_TOKEN": "x"} },
		"network":            func(r *sandboxprotocol.Request) { r.NetworkProfile = "bridge" },
		"memory":             func(r *sandboxprotocol.Request) { r.MemoryMiB = 2048 },
		"digest":             func(r *sandboxprotocol.Request) { r.RequestDigest = executorTestDigest },
	} {
		t.Run(name, func(t *testing.T) {
			engine := &fakeEngine{}
			executor := testExecutor(t, engine)
			request := validRequest(t, executor)
			mutate(&request)
			if name != "digest" {
				request.RequestDigest, _ = sandboxprotocol.Digest(request)
			}
			if _, err := executor.Execute(context.Background(), request); err == nil || engine.runs != 0 {
				t.Fatalf("invalid request was executed: err=%v runs=%d", err, engine.runs)
			}
		})
	}
}
