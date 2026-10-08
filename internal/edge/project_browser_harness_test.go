package edge

import "testing"

func TestNormalizeProjectBrowserHarnessRequests(t *testing.T) {
	start, err := normalizeProjectBrowserHarnessRequest(OperationProjectBrowserHarnessStart, OperationRequest{
		Alias: "MCP-DEVBOX", TargetAlias: "PARROT-TRUSTED-LINUX", Profile: "linux-workcell", IdempotencyKey: "harness-start-1",
		Argv: []string{"node", "tests/e2e.mjs"}, CWD: "tests", Environment: map[string]string{"CI": "true"},
		BrowserHarnessProfile: "signed-in", BrowserHarnessTimeoutSeconds: 3600, BrowserHarnessStorageMiB: 2048,
	})
	if err != nil || start.Alias != "mcp-devbox" || start.TargetAlias != "parrot-trusted-linux" || len(start.Argv) != 2 {
		t.Fatalf("start=%+v err=%v", start, err)
	}
	status, err := normalizeProjectBrowserHarnessRequest(OperationProjectBrowserHarnessStatus, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", BrowserHarnessRunID: "bh_11111111111111111111111111111111", StdoutOffset: 10, StderrOffset: 20, OutputLimit: 4096})
	if err != nil || status.OutputLimit != 4096 {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	artifact, err := normalizeProjectBrowserHarnessRequest(OperationProjectBrowserHarnessArtifactRead, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", BrowserHarnessRunID: "bh_11111111111111111111111111111111", BrowserHarnessArtifactPath: "artifacts/trace.zip", BrowserHarnessArtifactOffset: 0, BrowserHarnessArtifactLimit: 24576})
	if err != nil || artifact.BrowserHarnessArtifactPath != "artifacts/trace.zip" {
		t.Fatalf("artifact=%+v err=%v", artifact, err)
	}
}

func TestProjectBrowserHarnessRequestRejectsClosedOrUnsafeAuthority(t *testing.T) {
	bad := []struct {
		kind    OperationKind
		request OperationRequest
	}{
		{OperationProjectBrowserHarnessStart, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", Argv: []string{}, BrowserHarnessProfile: "default", BrowserHarnessTimeoutSeconds: 60, BrowserHarnessStorageMiB: 128}},
		{OperationProjectBrowserHarnessStart, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", Argv: []string{"node", "script.js"}, BrowserHarnessProfile: "../../host", BrowserHarnessTimeoutSeconds: 60, BrowserHarnessStorageMiB: 128}},
		{OperationProjectBrowserHarnessArtifactRead, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", BrowserHarnessRunID: "bh_11111111111111111111111111111111", BrowserHarnessArtifactPath: "../secret", BrowserHarnessArtifactLimit: 10}},
		{OperationProjectBrowserHarnessStart, OperationRequest{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", Argv: []string{"node", "script.js"}, BrowserHarnessProfile: "default", BrowserHarnessTimeoutSeconds: 60, BrowserHarnessStorageMiB: 128, BrowserSteps: []BrowserStep{{Action: "click", Selector: "#x"}}}},
	}
	for i, item := range bad {
		if _, err := normalizeProjectBrowserHarnessRequest(item.kind, item.request); err == nil {
			t.Fatalf("request %d accepted", i)
		}
	}
}

func TestProjectBrowserHarnessResultValidation(t *testing.T) {
	result := OperationResult{WorkspaceID: "ws_22222222222222222222222222222222", ProjectAlias: "mcp-devbox", ProjectOwner: "charle-z", ProjectRepository: "mcp-devbox", ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev", BrowserHarnessRunID: "bh_11111111111111111111111111111111", BrowserHarnessState: "running", BrowserHarnessProfile: "default", BrowserHarnessCreatedAt: "2026-08-04T04:00:00Z", BrowserHarnessUpdatedAt: "2026-08-04T04:00:01Z", BrowserHarnessTimeoutSeconds: 60, BrowserHarnessStorageMiB: 128, BrowserHarnessStdout: "ok\n", BrowserHarnessStdoutNext: 3, BrowserHarnessStdoutEOF: true, BrowserHarnessStderrEOF: true, BrowserHarnessArtifactCount: 1, BrowserHarnessArtifactBytes: 10}
	if !validProjectBrowserHarnessResultForKind(OperationProjectBrowserHarnessStatus, result) {
		t.Fatalf("valid result rejected: %+v", result)
	}
	result.BrowserHarnessState = "host-root"
	if validProjectBrowserHarnessResultForKind(OperationProjectBrowserHarnessStatus, result) {
		t.Fatal("invalid state accepted")
	}
}
