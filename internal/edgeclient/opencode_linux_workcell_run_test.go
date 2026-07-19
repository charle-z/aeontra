//go:build !windows

package edgeclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestOpenCodeLauncherExposesPrivateHTBLabBrokerDuringRuntime(t *testing.T) {
	fixture, workspace, _, lease, _ := linuxWorkcellLauncherFixture(t, WorkspaceModeHTBLinux)
	fixture.remote.runtime.WorkspaceID = workspace.ID
	fixture.launcher.linuxNetworkProbe = fakeLinuxNetworkProbe{ipv4: "10.10.14.9", routeInterface: "tun0"}
	brokerObserved := false
	runtimeSource := ""
	fixture.launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		for _, mount := range spec.Sandbox.Mounts {
			if mount.Target == openCodeSandboxRuntime {
				runtimeSource = mount.Source
				break
			}
		}
		if runtimeSource == "" {
			t.Fatal("runtime mount missing")
		}
		if !strings.Contains(spec.Sandbox.Environment["OPENCODE_CONFIG_CONTENT"], "workspace_htb_status") {
			t.Fatal("structured HTB tools missing from provider configuration")
		}
		info, err := os.Lstat(filepath.Join(runtimeSource, HTBLabBrokerSocketName))
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
			t.Fatalf("broker socket mode=%v err=%v", infoMode(info), err)
		}
		brokerObserved = true
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := fixture.launcher.RunLease(context.Background(), lease)
	if err != nil || result.State != OpenCodeLocalCompleted || !brokerObserved {
		t.Fatalf("result=%+v observed=%t err=%v", result, brokerObserved, err)
	}
	if _, err := os.Lstat(filepath.Join(runtimeSource, HTBLabBrokerSocketName)); !os.IsNotExist(err) {
		t.Fatalf("broker socket survived runtime cleanup: %v", err)
	}
}
