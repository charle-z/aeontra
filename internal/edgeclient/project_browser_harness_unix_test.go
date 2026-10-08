//go:build !windows

package edgeclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBrowserHarnessManager(t *testing.T) (*ProjectToolboxManager, *recordingToolboxRunner, Workspace) {
	t.Helper()
	stateRoot := t.TempDir()
	workspace := Workspace{ID: "ws_22222222222222222222222222222222", Path: t.TempDir(), Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	runner := &recordingToolboxRunner{workspace: workspace.Path, socket: filepath.Join(stateRoot, "podman.sock")}
	manager, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{
		StateRoot:    stateRoot,
		Endpoint:     &RootlessContainerEndpoint{Engine: "podman", SocketPath: runner.socket, Executable: "/usr/bin/podman"},
		Runner:       runner,
		environment:  testRootlessContainerEnvironment,
		NewID:        func() (string, error) { return "tb_11111111111111111111111111111111", nil },
		NewHarnessID: func() (string, error) { return "bh_44444444444444444444444444444444", nil },
		Now:          func() time.Time { return time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.Create(context.Background(), ProjectToolboxCreateRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	return manager, runner, workspace
}

func TestProjectBrowserHarnessRunsArbitraryArgvAndPersistsManagedState(t *testing.T) {
	manager, runner, workspace := testBrowserHarnessManager(t)
	request := ProjectBrowserHarnessStartRequest{
		ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace,
		IdempotencyKey: "browser-harness-run-1", Profile: "signed-in",
		Argv: []string{"node", "tests/e2e.mjs", "--project=chromium"}, CWD: "tests", Environment: map[string]string{"CI": "true"},
		TimeoutSeconds: 3600, StorageMiB: 2048,
	}
	started, reused, err := manager.BrowserHarnessStart(context.Background(), request)
	if err != nil || reused || started.RunID != "bh_44444444444444444444444444444444" || started.State != "running" || started.Profile != "signed-in" {
		t.Fatalf("started=%+v reused=%v err=%v", started, reused, err)
	}
	var call string
	for _, item := range runner.calls {
		joined := strings.Join(item, " ")
		if strings.Contains(joined, "mcp-browser-harness-start") {
			call = joined
		}
	}
	for _, required := range []string{"exec --detach --workdir /workspace/tests", "--env CI=true", "--env MCP_BROWSER_RUN_ID=bh_44444444444444444444444444444444", "--env MCP_BROWSER_RUN_DIR=/workspace/.mcp-devbox/browser-harness/runs/bh_44444444444444444444444444444444", "--env MCP_BROWSER_PROFILE_DIR=/workspace/.mcp-devbox/browser-harness/profiles/signed-in", "--env PLAYWRIGHT_BROWSERS_PATH=/var/lib/mcp-devbox/browser-browsers", "mcp-browser-harness-start", "3600", "2147483648", "node tests/e2e.mjs --project=chromium"} {
		if !strings.Contains(call, required) {
			t.Fatalf("missing %q in %q", required, call)
		}
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", started.RunID)
	profileRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "profiles", "signed-in")
	for _, path := range []string{runRoot, filepath.Join(runRoot, "artifacts"), filepath.Join(runRoot, "downloads"), profileRoot} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			t.Fatalf("path=%s info=%+v err=%v", path, info, err)
		}
	}
	if err := os.WriteFile(filepath.Join(runRoot, "stdout.log"), []byte("browser-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runRoot, "stderr.log"), []byte("console-warning\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(runRoot, "artifacts", "trace.zip")
	if err := os.WriteFile(artifact, []byte("trace-body"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := manager.BrowserHarnessStatus(context.Background(), ProjectBrowserHarnessStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, StdoutOffset: 0, StderrOffset: 0, Limit: 1024})
	if err != nil || status.Stdout != "browser-ok\n" || status.Stderr != "console-warning\n" || status.ArtifactCount != 1 || status.ArtifactBytes != int64(len("trace-body")) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	artifacts, err := manager.BrowserHarnessArtifactList(ProjectBrowserHarnessArtifactListRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Limit: 20})
	if err != nil || len(artifacts) != 1 || artifacts[0].Path != "artifacts/trace.zip" || artifacts[0].MediaType != "application/zip" {
		t.Fatalf("artifacts=%+v err=%v", artifacts, err)
	}
	chunk, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "artifacts/trace.zip", Offset: 0, Limit: 1024})
	if err != nil || chunk.DataBase64 != base64.StdEncoding.EncodeToString([]byte("trace-body")) || !chunk.EOF {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}

	reopened, err := OpenProjectToolboxManager(ProjectToolboxManagerConfig{StateRoot: filepath.Dir(manager.stateRoot), Endpoint: manager.endpoint, Runner: runner, environment: testRootlessContainerEnvironment})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.BrowserHarnessStatus(context.Background(), ProjectBrowserHarnessStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Limit: 1024})
	if err != nil || recovered.RunID != started.RunID || recovered.State != "running" {
		t.Fatalf("recovered=%+v err=%v", recovered, err)
	}
	stopped, err := reopened.BrowserHarnessStop(context.Background(), ProjectBrowserHarnessStopRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, GraceSeconds: 3})
	if err != nil || stopped.State != "stopped" {
		t.Fatalf("stopped=%+v err=%v", stopped, err)
	}
	cleaned, err := reopened.BrowserHarnessCleanup(ProjectBrowserHarnessCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, RemoveProfile: true})
	if err != nil || cleaned.Runs != 1 || cleaned.Artifacts != 1 || cleaned.Profiles != 1 {
		t.Fatalf("cleaned=%+v err=%v", cleaned, err)
	}
	if _, err := os.Stat(runRoot); !os.IsNotExist(err) {
		t.Fatalf("run root still exists: %v", err)
	}
	if _, err := os.Stat(profileRoot); !os.IsNotExist(err) {
		t.Fatalf("profile root still exists: %v", err)
	}
}

func TestProjectBrowserHarnessIdempotencyAndIndeterminateRecovery(t *testing.T) {
	manager, runner, workspace := testBrowserHarnessManager(t)
	request := ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "same-run", Profile: "default", Argv: []string{"python3", "script.py"}, TimeoutSeconds: 60, StorageMiB: 128}
	first, reused, err := manager.BrowserHarnessStart(context.Background(), request)
	if err != nil || reused {
		t.Fatalf("first=%+v reused=%v err=%v", first, reused, err)
	}
	second, reused, err := manager.BrowserHarnessStart(context.Background(), request)
	if err != nil || !reused || second.RunID != first.RunID || countToolboxCalls(runner.calls, "mcp-browser-harness-start") != 1 {
		t.Fatalf("second=%+v reused=%v err=%v calls=%v", second, reused, err, runner.calls)
	}
	runner.harnessState = "indeterminate"
	status, err := manager.BrowserHarnessStatus(context.Background(), ProjectBrowserHarnessStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: first.RunID, Limit: 1024})
	if err != nil || status.State != "indeterminate" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	third, reused, err := manager.BrowserHarnessStart(context.Background(), request)
	if err != nil || !reused || third.State != "indeterminate" || countToolboxCalls(runner.calls, "mcp-browser-harness-start") != 1 {
		t.Fatalf("third=%+v reused=%v err=%v", third, reused, err)
	}
}

func TestProjectBrowserHarnessArtifactBoundaryRejectsTraversalAndSymlink(t *testing.T) {
	manager, _, workspace := testBrowserHarnessManager(t)
	started, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "artifact-boundary", Profile: "default", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "../secret", Limit: 10}); err == nil {
		t.Fatal("traversal accepted")
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", started.RunID)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(runRoot, "artifacts", "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "artifacts/link.txt", Limit: 10}); err == nil {
		t.Fatal("symlink artifact accepted")
	}
}

func TestProjectBrowserHarnessArtifactInventoryFailsClosedAtCountLimit(t *testing.T) {
	manager, _, workspace := testBrowserHarnessManager(t)
	started, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "artifact-count-limit", Profile: "default", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", started.RunID)
	for index := 0; index <= projectBrowserHarnessMaxArtifacts; index++ {
		name := filepath.Join(runRoot, "artifacts", fmt.Sprintf("artifact-%03d.txt", index))
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.BrowserHarnessArtifactList(ProjectBrowserHarnessArtifactListRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Limit: projectBrowserHarnessMaxArtifacts}); err == nil {
		t.Fatal("artifact inventory above the global count cap was accepted")
	}
}

func TestProjectBrowserHarnessArtifactRejectsOversizedSparseFile(t *testing.T) {
	manager, _, workspace := testBrowserHarnessManager(t)
	started, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "artifact-size-limit", Profile: "default", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", started.RunID)
	path := filepath.Join(runRoot, "artifacts", "oversized.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(projectBrowserHarnessMaxFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.BrowserHarnessArtifactList(ProjectBrowserHarnessArtifactListRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Limit: 1}); err == nil {
		t.Fatal("oversized artifact was listed")
	}
	if _, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "artifacts/oversized.bin", Limit: 1}); err == nil {
		t.Fatal("oversized artifact was read")
	}
}

func TestProjectBrowserHarnessArtifactDigestCacheInvalidatesOnChange(t *testing.T) {
	manager, _, workspace := testBrowserHarnessManager(t)
	started, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "artifact-digest-cache", Profile: "default", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", started.RunID)
	path := filepath.Join(runRoot, "artifacts", "result.txt")
	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "artifacts/result.txt", Limit: 16})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(path, []byte("second-longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := manager.BrowserHarnessArtifactRead(ProjectBrowserHarnessArtifactReadRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: started.RunID, Path: "artifacts/result.txt", Limit: 16})
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 == second.SHA256 || second.DataBase64 != base64.StdEncoding.EncodeToString([]byte("second-longer")) {
		t.Fatalf("stale digest cache reused: first=%+v second=%+v", first, second)
	}
}

func TestProjectBrowserHarnessCleanupPreservesSharedProfile(t *testing.T) {
	manager, runner, workspace := testBrowserHarnessManager(t)
	ids := []string{"bh_44444444444444444444444444444444", "bh_55555555555555555555555555555555"}
	index := 0
	manager.newHarnessID = func() (string, error) { id := ids[index]; index++; return id, nil }
	first, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "shared-1", Profile: "shared", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	runner.harnessState = "stopped"
	if _, err := manager.BrowserHarnessStatus(context.Background(), ProjectBrowserHarnessStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: first.RunID, Limit: 1024}); err != nil {
		t.Fatal(err)
	}
	runner.harnessState = "running"
	second, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "shared-2", Profile: "shared", Argv: []string{"sleep", "60"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := manager.BrowserHarnessCleanup(ProjectBrowserHarnessCleanupRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: first.RunID, RemoveProfile: true})
	if err != nil || cleaned.Runs != 1 || cleaned.Profiles != 0 {
		t.Fatalf("cleaned=%+v err=%v", cleaned, err)
	}
	profile := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "profiles", "shared")
	if info, err := os.Stat(profile); err != nil || !info.IsDir() {
		t.Fatalf("shared profile removed info=%+v err=%v second=%+v", info, err, second)
	}
}

func TestProjectBrowserHarnessCountsAllArtifactsAndReadsEOF(t *testing.T) {
	manager, _, workspace := testBrowserHarnessManager(t)
	run, _, err := manager.BrowserHarnessStart(context.Background(), ProjectBrowserHarnessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, IdempotencyKey: "count-all", Profile: "default", Argv: []string{"true"}, TimeoutSeconds: 60, StorageMiB: 128})
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(workspace.Path, ".mcp-devbox", "browser-harness", "runs", run.RunID)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(runRoot, "artifacts", name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	status, err := manager.BrowserHarnessStatus(context.Background(), ProjectBrowserHarnessStatusRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: run.RunID, StdoutOffset: 0, StderrOffset: 0, Limit: 1024})
	if err != nil || status.ArtifactCount != 3 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	list, err := manager.BrowserHarnessArtifactList(ProjectBrowserHarnessArtifactListRequest{ProjectAlias: "project", TargetAlias: "parrot", Workspace: workspace, RunID: run.RunID, Limit: 1})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	stdoutPath := filepath.Join(runRoot, "stdout.log")
	info, err := os.Stat(stdoutPath)
	if err != nil {
		t.Fatal(err)
	}
	text, next, eof, truncated, err := readProjectBrowserHarnessLog(runRoot, "stdout.log", info.Size(), 1024)
	if err != nil || text != "" || next != info.Size() || !eof || truncated {
		t.Fatalf("text=%q next=%d eof=%v truncated=%v err=%v", text, next, eof, truncated, err)
	}
}
