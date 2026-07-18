//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeLauncherRunsLinuxWorkcellFromLocalRegistryContract(t *testing.T) {
	fixture, workspace, _, lease, _ := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	fixture.remote.runtime.WorkspaceID = workspace.ID
	var captured openCodeProcessSpec
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		captured = spec
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := fixture.launcher.RunLease(context.Background(), lease)
	if err != nil || result.State != OpenCodeLocalCompleted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !captured.Sandbox.ShareNetwork || captured.Sandbox.Environment["MCP_DEVBOX_PROFILE"] != "linux-workcell" {
		t.Fatalf("captured sandbox=%+v", captured.Sandbox)
	}
	statePath := filepath.Join(workspace.Path, ".mcp-devbox", "current-state.md")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("durable state missing: %v", err)
	}
}
