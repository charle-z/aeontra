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
	mu         sync.Mutex
	attest     error
	readyErr   error
	readyCalls int
	runs       int
	lastSpec   RunSpec
	response   sandboxprotocol.Response
	err        error
	started    chan struct{}
	release    chan struct{}
}

func (f *fakeEngine) Attest(context.Context, string, string) error { return f.attest }
func (f *fakeEngine) Ready(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readyCalls++
	return f.readyErr
}
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
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	stateRoot := t.TempDir()
	// testing.TempDir creates numbered child directories with 0777 before
	// umask. Production requires an existing operator-owned 0700 state root,
	// so the fixture must model that boundary explicitly.
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	executor, err := New(Config{
		Token: strings.Repeat("t", 32), WorkspaceID: "primary", WorkspaceRoot: root, StateRoot: stateRoot,
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
		SchemaVersion: 1, ProfileVersion: sandboxprotocol.ProfileVersion,
		IdempotencyKey: "sx_0123456789abcdef0123456789abcdef",
		WorkspaceID:    "primary", RelativeDir: "", Argv: []string{"bash", "-lc", "cargo test"},
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

func TestStatusRequiresRealReadinessExecution(t *testing.T) {
	executor := testExecutor(t, &fakeEngine{readyErr: errors.New("cannot start container")})
	status := executor.Status(context.Background())
	if status.Available || status.Rootless {
		t.Fatalf("executor without a working readiness command reported available: %#v", status)
	}
}

func TestExecuteRequiresRealReadinessExecution(t *testing.T) {
	engine := &fakeEngine{readyErr: errors.New("cannot start container")}
	executor := testExecutor(t, engine)
	request := validRequest(t, executor)
	if _, err := executor.Execute(context.Background(), request); err == nil || engine.runs != 0 || engine.readyCalls != 1 {
		t.Fatalf("execution bypassed readiness: err=%v runs=%d readiness=%d", err, engine.runs, engine.readyCalls)
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

func TestReceiptPathStaysWithinPrivateState(t *testing.T) {
	executor := testExecutor(t, &fakeEngine{})
	key := "sx_0123456789abcdef0123456789abcdef"
	got, err := executor.receiptPath(key, ".done")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(executor.config.StateRoot, "receipts", key+".done")
	if got != want {
		t.Fatalf("receipt path = %q, want %q", got, want)
	}
	for _, input := range []struct {
		key    string
		suffix string
	}{
		{key: "../outside", suffix: ".done"},
		{key: key + "/outside", suffix: ".done"},
		{key: key, suffix: "/outside"},
		{key: key, suffix: ".unknown"},
	} {
		if path, err := executor.receiptPath(input.key, input.suffix); err == nil {
			t.Fatalf("unsafe receipt path accepted: %q", path)
		}
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

func TestExecuteScopesPreflightAndMountToSelectedWorkspace(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0, Stdout: "ok"}}
	executor := testExecutor(t, engine)
	if err := os.RemoveAll(filepath.Join(executor.config.WorkspaceRoot, ".git")); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(executor.config.WorkspaceRoot, "selected")
	sibling := filepath.Join(executor.config.WorkspaceRoot, "sibling")
	if err := os.MkdirAll(filepath.Join(selected, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, ".env"), []byte("TOKEN=value"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, executor)
	request.WorkspaceScope = "selected"
	request.RelativeDir = "sub"
	request.RequestDigest, _ = sandboxprotocol.Digest(request)
	if _, err := executor.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if engine.lastSpec.WorkspaceRoot != selected || engine.lastSpec.RelativeDir != "sub" {
		t.Fatalf("selected workspace was not isolated: %#v", engine.lastSpec)
	}
}

func TestExecuteAcceptsLegacyRelativeWorkspaceSelection(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0, Stdout: "ok"}}
	executor := testExecutor(t, engine)
	if err := os.RemoveAll(filepath.Join(executor.config.WorkspaceRoot, ".git")); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(executor.config.WorkspaceRoot, "selected")
	if err := os.MkdirAll(filepath.Join(selected, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, executor)
	request.ProfileVersion = ""
	request.RelativeDir = "selected/sub"
	request.RequestDigest, _ = sandboxprotocol.Digest(request)
	if _, err := executor.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if engine.lastSpec.WorkspaceRoot != selected || engine.lastSpec.RelativeDir != "sub" {
		t.Fatalf("legacy workspace selection changed: %#v", engine.lastSpec)
	}
}

func TestExecuteAcceptsCasePreservingWorkspaceScope(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0}}
	executor := testExecutor(t, engine)
	if err := os.RemoveAll(filepath.Join(executor.config.WorkspaceRoot, ".git")); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(executor.config.WorkspaceRoot, "OpenAI-Codex")
	if err := os.MkdirAll(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, executor)
	request.WorkspaceScope = "OpenAI-Codex"
	request.RequestDigest, _ = sandboxprotocol.Digest(request)
	if _, err := executor.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if engine.lastSpec.WorkspaceRoot != selected {
		t.Fatalf("case-preserving selected workspace changed: %#v", engine.lastSpec)
	}
}

func TestExecuteKeepsRelativeDirectoryInsideDirectRepository(t *testing.T) {
	engine := &fakeEngine{response: sandboxprotocol.Response{ExitCode: 0}}
	executor := testExecutor(t, engine)
	if err := os.MkdirAll(filepath.Join(executor.config.WorkspaceRoot, "pkg", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, executor)
	request.RelativeDir = "pkg/sub"
	request.RequestDigest, _ = sandboxprotocol.Digest(request)
	if _, err := executor.Execute(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if engine.lastSpec.WorkspaceRoot != executor.config.WorkspaceRoot || engine.lastSpec.RelativeDir != "pkg/sub" {
		t.Fatalf("direct repository selection changed: %#v", engine.lastSpec)
	}
}

func TestResolveWorkspaceSelectionRejectsMissingEscapingAndSymlinkScopes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "repo"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]struct {
		scope       string
		relativeDir string
	}{
		"missing scope":      {scope: "missing"},
		"scope traversal":    {scope: "../outside"},
		"relative traversal": {scope: "repo", relativeDir: "../outside"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := resolveWorkspaceSelection(root, sandboxprotocol.ProfileVersion, input.scope, input.relativeDir); err == nil {
				t.Fatalf("unsafe workspace selection was accepted: %#v", input)
			}
		})
	}
	t.Run("symlink scope", func(t *testing.T) {
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
			if errors.Is(err, os.ErrPermission) {
				t.Skip("symlink creation is not permitted on this host")
			}
			t.Fatal(err)
		}
		if _, _, err := resolveWorkspaceSelection(root, sandboxprotocol.ProfileVersion, "link", ""); err == nil {
			t.Fatal("symlink workspace scope was accepted")
		}
	})
}

func TestLegacyRequestDerivesScopeOnlyFromRelativeDirectory(t *testing.T) {
	root := t.TempDir()
	selected := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(selected, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	workspace, relative, err := resolveWorkspaceSelection(root, sandboxprotocol.LegacyProfileVersion, "", "repo/pkg")
	if err != nil || workspace != selected || relative != "pkg" {
		t.Fatalf("legacy selection workspace=%q relative=%q err=%v", workspace, relative, err)
	}
	if _, _, err := resolveWorkspaceSelection(root, sandboxprotocol.LegacyProfileVersion, "repo", ""); err == nil {
		t.Fatal("legacy request accepted an l3-v2 workspace scope")
	}
}

func TestScanWorkspaceObservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scanWorkspace(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan err=%v", err)
	}
}

func TestExecuteRejectsSecretOnlyInsideSelectedWorkspace(t *testing.T) {
	engine := &fakeEngine{}
	executor := testExecutor(t, engine)
	if err := os.RemoveAll(filepath.Join(executor.config.WorkspaceRoot, ".git")); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(executor.config.WorkspaceRoot, "selected")
	if err := os.MkdirAll(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected, ".env"), []byte("TOKEN=value"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := validRequest(t, executor)
	request.WorkspaceScope = "selected"
	request.RequestDigest, _ = sandboxprotocol.Digest(request)
	if _, err := executor.Execute(context.Background(), request); err == nil || !strings.Contains(err.Error(), "secret") || engine.runs != 0 {
		t.Fatalf("selected secret workspace was not rejected: err=%v runs=%d", err, engine.runs)
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
