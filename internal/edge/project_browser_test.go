package edge

import "testing"

func TestNormalizeProjectBrowserRequests(t *testing.T) {
	create, err := normalizeProjectBrowserRequest(OperationProjectBrowserCreate, OperationRequest{
		Alias: "MCP-DEVBOX", TargetAlias: "PARROT-TRUSTED-LINUX", Profile: "linux-workcell",
		IdempotencyKey: "browser-create-1", BrowserNetworkScope: "general", BrowserInitialURL: "https://example.com/path?token=private",
		BrowserViewportWidth: 1280, BrowserViewportHeight: 720,
	})
	if err != nil || create.Alias != "mcp-devbox" || create.TargetAlias != "parrot-trusted-linux" || create.BrowserNetworkScope != "general" {
		t.Fatalf("create=%+v err=%v", create, err)
	}
	run, err := normalizeProjectBrowserRequest(OperationProjectBrowserRun, OperationRequest{
		Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "browser-run-1",
		BrowserSessionID: "br_0123456789abcdef0123456789abcdef", BrowserTimeoutSeconds: 60,
		BrowserCapture: "both", BrowserSteps: []BrowserStep{{Action: "navigate", URL: "https://example.com"}, {Action: "click", Selector: "#ready", SelectorType: "css"}},
	})
	if err != nil || len(run.BrowserSteps) != 2 || run.BrowserCapture != "both" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestProjectBrowserRequestsRejectOpenAuthority(t *testing.T) {
	bad := []OperationRequest{
		{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", BrowserNetworkScope: "general", BrowserInitialURL: "file:///etc/passwd"},
		{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", BrowserNetworkScope: "isolated", BrowserInitialURL: "http://127.0.0.1:22"},
		{Alias: "mcp-devbox", TargetAlias: "parrot-trusted-linux", Profile: "linux-workcell", IdempotencyKey: "x", BrowserSessionID: "br_0123456789abcdef0123456789abcdef", BrowserTimeoutSeconds: 60, BrowserSteps: []BrowserStep{{Action: "evaluate", Text: "document.cookie"}}},
	}
	kinds := []OperationKind{OperationProjectBrowserCreate, OperationProjectBrowserCreate, OperationProjectBrowserRun}
	for i := range bad {
		if _, err := normalizeProjectBrowserRequest(kinds[i], bad[i]); err == nil {
			t.Fatalf("request %d accepted", i)
		}
	}
}

func TestProjectBrowserResultValidation(t *testing.T) {
	result := OperationResult{
		WorkspaceID: "ws_0123456789abcdef0123456789abcdef", ProjectAlias: "mcp-devbox", ProjectOwner: "charle-z", ProjectRepository: "mcp-devbox",
		ProjectTarget: "parrot-trusted-linux", ProjectState: "ready", ProjectProfile: "linux-workcell", ProjectMode: "dev",
		BrowserSessionID: "br_0123456789abcdef0123456789abcdef", BrowserState: "ready", BrowserNetworkScope: "general",
		BrowserSafeURL: "https://example.com/path", BrowserTitle: "Example", BrowserRevision: 2,
		BrowserCreatedAt: "2026-08-04T02:00:00Z", BrowserUpdatedAt: "2026-08-04T02:00:01Z",
	}
	if !validProjectBrowserResultForKind(OperationProjectBrowserStatus, result) {
		t.Fatalf("valid result rejected: %+v", result)
	}
	result.BrowserSafeURL = "https://example.com/path?secret=value"
	if validProjectBrowserResultForKind(OperationProjectBrowserStatus, result) {
		t.Fatal("query-bearing safe URL accepted")
	}
}
