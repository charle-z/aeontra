package edgeclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBrowserRunner struct {
	result BrowserPageResult
	err    error
	runs   int
}

func (r *fakeBrowserRunner) Run(_ context.Context, request BrowserPageRequest) (BrowserPageResult, error) {
	r.runs++
	return r.result, r.err
}

func testBrowserResolution(t *testing.T) ProjectResolution {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return ProjectResolution{Project: Project{Alias: "mcp-devbox", Owner: "charle-z", Repository: "mcp-devbox"}, TargetAlias: "parrot-trusted-linux", Workspace: Workspace{ID: "ws_0123456789abcdef0123456789abcdef", Path: root, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}}
}

func TestProjectBrowserManagerPersistsSessionAndArtifact(t *testing.T) {
	resolution := testBrowserResolution(t)
	runner := &fakeBrowserRunner{result: BrowserPageResult{URL: "https://example.com/path?secret=value", Title: "Example", Text: "ready", Screenshot: []byte("jpeg")}}
	root := filepath.Join(t.TempDir(), "state")
	manager, err := OpenProjectBrowserManager(ProjectBrowserManagerConfig{Root: root, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	created, reused, err := manager.Create(context.Background(), ProjectBrowserCreateRequest{IdempotencyKey: "create-1", Resolution: resolution, NetworkScope: "general", InitialURL: "https://example.com", ViewportWidth: 1280, ViewportHeight: 720})
	if err != nil || reused || created.SessionID == "" || created.SafeURL != "https://example.com" {
		t.Fatalf("created=%+v reused=%v err=%v", created, reused, err)
	}
	runRequest := ProjectBrowserRunRequest{OperationID: "eo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IdempotencyKey: "run-1", Resolution: resolution, SessionID: created.SessionID, Steps: []BrowserStep{{Action: "wait", Milliseconds: 1}}, Capture: "both", TimeoutSeconds: 30}
	run, err := manager.Run(context.Background(), runRequest)
	if err != nil || run.ArtifactID == "" || run.SafeURL != "https://example.com/path" || run.Text != "ready" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	chunk, err := manager.ReadArtifact(ProjectBrowserArtifactReadRequest{Resolution: resolution, SessionID: created.SessionID, ArtifactID: run.ArtifactID, Offset: 0, Limit: 16})
	if err != nil || !chunk.EOF || chunk.DataBase64 == "" {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProjectBrowserManager(ProjectBrowserManagerConfig{Root: root, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	status, err := reopened.Status(ProjectBrowserReadRequest{Resolution: resolution, SessionID: created.SessionID})
	if err != nil || status.Revision != run.Revision {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	replayed, err := reopened.Run(context.Background(), runRequest)
	if err != nil || replayed.Revision != run.Revision || runner.runs != 1 {
		t.Fatalf("replayed=%+v runs=%d err=%v", replayed, runner.runs, err)
	}
}

func TestBrowserURLPolicyUsesGeneralWorkcellNetwork(t *testing.T) {
	for _, raw := range []string{"https://example.com", "http://127.0.0.1:3000", "http://10.0.0.5:8080", "https://localhost:9443"} {
		if err := ValidateBrowserURL(context.Background(), "general", "", raw, nil); err != nil {
			t.Fatalf("url=%s err=%v", raw, err)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)"} {
		if err := ValidateBrowserURL(context.Background(), "general", "", raw, nil); err == nil {
			t.Fatalf("url=%s accepted", raw)
		}
	}
}

func TestProjectBrowserLauncherUsesNarrowBubblewrapBoundary(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	args, err := projectBrowserBubblewrapArgs(profile, []string{"--remote-debugging-port=0", "--user-data-dir=" + profile})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, required := range []string{"--unshare-all", "--share-net", "--tmpfs", "/tmp", "--bind", profile, "/browser-profile", "/usr/lib/chromium/chromium", "--user-data-dir=/browser-profile"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing %q in %v", required, args)
		}
	}
	for _, forbidden := range []string{"/workspace", "/var/run/docker.sock", "/run/user/", "/mnt/c", "/root/.config", "--remote-debugging-address=0.0.0.0"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("exposed %q in %v", forbidden, args)
		}
	}
}

func TestProjectBrowserLauncherRejectsUnsafeChromeFlags(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--user-data-dir=/tmp/other"}, {"--remote-debugging-address=0.0.0.0"}, {"--load-extension=/tmp/x"}} {
		if _, err := projectBrowserBubblewrapArgs(profile, args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestPrivateBrowserCleanupRejectsEscapesAndSymlinks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profiles")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sessionID := "br_eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	profile := filepath.Join(root, sessionID)
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateBrowserProfile(profile, root, sessionID); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, profile); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateBrowserProfile(profile, root, sessionID); err == nil {
		t.Fatal("symlink profile accepted")
	}
	artifactRoot := filepath.Join(t.TempDir(), "artifacts")
	if err := os.Mkdir(artifactRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removePrivateBrowserArtifact(filepath.Join(artifactRoot, "nested", "ba_ffffffffffffffffffffffffffffffff.jpg"), artifactRoot); err == nil {
		t.Fatal("nested artifact accepted")
	}
}

func TestProjectBrowserRunFailureBecomesIndeterminate(t *testing.T) {
	resolution := testBrowserResolution(t)
	runner := &fakeBrowserRunner{err: errors.New("browser crashed")}
	manager, err := OpenProjectBrowserManager(ProjectBrowserManagerConfig{Root: filepath.Join(t.TempDir(), "state"), Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	created, _, err := manager.Create(context.Background(), ProjectBrowserCreateRequest{IdempotencyKey: "create-failure", Resolution: resolution, NetworkScope: "general", InitialURL: "https://example.com", ViewportWidth: 1280, ViewportHeight: 720})
	if err != nil {
		t.Fatal(err)
	}
	request := ProjectBrowserRunRequest{OperationID: "eo_ffffffffffffffffffffffffffffffff", IdempotencyKey: "run-failure", Resolution: resolution, SessionID: created.SessionID, Steps: []BrowserStep{{Action: "wait", Milliseconds: 1}}, Capture: "none", TimeoutSeconds: 30}
	if _, err := manager.Run(context.Background(), request); err == nil {
		t.Fatal("failed browser run accepted")
	}
	if _, err := manager.Run(context.Background(), request); err == nil || !strings.Contains(err.Error(), "indeterminate") {
		t.Fatalf("retry err=%v", err)
	}
	if runner.runs != 1 {
		t.Fatalf("runner runs=%d want=1", runner.runs)
	}
	status, err := manager.Status(ProjectBrowserReadRequest{Resolution: resolution, SessionID: created.SessionID})
	if err != nil || status.State != "ready" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
