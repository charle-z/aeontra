//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeLauncherCleansRootlessResourcesAndRecordsCompletion(t *testing.T) {
	fixture, workspace, _, lease, _ := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	fixture.remote.runtime.WorkspaceID = workspace.ID
	fixture.launcher.effectiveUID = func() int { return 1000 }
	fixture.launcher.rootlessEndpoint = func(int, string) (*RootlessContainerEndpoint, error) {
		return &RootlessContainerEndpoint{Engine: "docker", SocketPath: "/run/user/1000/docker.sock", Executable: "/usr/bin/docker"}, nil
	}
	runner := &fakeContainerRunner{}
	fixture.launcher.containerRunner = runner
	fixture.launcher.rootlessEnvironment = testRootlessContainerEnvironment
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		if spec.Sandbox.Environment["MCP_DEVBOX_CONTAINER_LABEL"] != rootlessRuntimeLabelKey+"="+lease.RuntimeID {
			t.Fatalf("missing runtime label: %+v", spec.Sandbox.Environment)
		}
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := fixture.launcher.RunLease(context.Background(), lease)
	if err != nil || result.State != OpenCodeLocalCompleted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(runner.commands) != 6 {
		t.Fatalf("cleanup commands=%d", len(runner.commands))
	}
	runtimeRoots, err := prepareProjectRuntimeRoots(fixture.launcher.config.StateRoot, workspace)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(projectRuntimeControlRoot(runtimeRoots), "current-state.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Runtime state: completed") || !strings.Contains(string(content), "Container cleanup: complete:") {
		t.Fatalf("terminal state=%s", content)
	}
}
