//go:build !windows

package edgeclient

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/modelturn"
)

func TestCodexLinuxWorkcellSpecUsesOnlySignedHarnessAndLoopbackAdapter(t *testing.T) {
	fixture := newOpenCodeLauncherFixture(t)
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace := Workspace{ID: fixture.lease.WorkspaceID, Path: fixture.workspace, Profile: WorkspaceProfileLinuxWorkcell, Mode: WorkspaceModeDev}
	prepared := LinuxWorkcellPreparation{Workspace: workspace}
	spec, err := launcher.codexLinuxWorkcellProcessSpec(filepath.Join(fixture.state, "r", fixture.lease.RuntimeID), workspace, prepared, "http://127.0.0.1:43210/v1", fixture.lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{codexSandboxExecutable, "exec", "--ignore-user-config", "--ephemeral", "--skip-git-repo-check", "--sandbox", "danger-full-access", "--cd", openCodeSandboxWorkspace}
	if !slices.Equal(spec.Sandbox.Command[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("command prefix=%v want=%v", spec.Sandbox.Command, wantPrefix)
	}
	joined := strings.Join(spec.Sandbox.Command, " ")
	for _, required := range []string{
		`model_provider="mcp-devbox"`,
		`model_providers.mcp-devbox.base_url="http://127.0.0.1:43210/v1"`,
		`model_providers.mcp-devbox.wire_api="responses"`,
		`model_providers.mcp-devbox.requires_openai_auth=false`,
		`agents.enabled=false`,
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Codex command is missing %q: %s", required, joined)
		}
	}
	for key := range spec.Sandbox.Environment {
		if strings.Contains(strings.ToUpper(key), "OPENAI") || strings.Contains(strings.ToUpper(key), "API_KEY") {
			t.Fatalf("credential-shaped environment escaped into Codex: %s", key)
		}
	}
	if mount := findSandboxMount(spec.Sandbox.Mounts, codexSandboxExecutable); mount.Source != codexPath || mount.Writable {
		t.Fatalf("Codex executable mount=%+v", mount)
	}
}

func TestCodexLauncherCompletesOneDurableLinuxWorkcellLease(t *testing.T) {
	fixture, workspace, _, lease, _ := linuxWorkcellLauncherFixture(t, WorkspaceModeDev)
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nprintf 'codex-cli 0.147.0\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.state, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: fixture.bubblewrap, OutputLimit: 4096, Heartbeat: time.Second,
		Workspaces: fixture.registry, Journal: fixture.journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	launcher.allowRootTest = true
	launcher.config.RuntimeStartupBudget = 0
	launcher.rootlessEndpoint = nil
	launcher.verifyCodexInstallation = func(string, string) error { return nil }
	launcher.verifySandbox = func(context.Context, openCodeProcessSpec) error { return nil }
	fixture.remote.runtime.WorkspaceID = workspace.ID
	launcher.remoteFactory = func(ModelRuntimeLease) (OpenCodeRemoteTransport, error) { return fixture.remote, nil }
	var captured openCodeProcessSpec
	launcher.runProcess = func(_ context.Context, spec openCodeProcessSpec) openCodeProcessResult {
		captured = spec
		if spec.Started == nil {
			t.Fatal("Codex process start observer is missing")
		}
		if err := spec.Started(); err != nil {
			t.Fatal(err)
		}
		return openCodeProcessResult{ExitCode: 0}
	}
	result, err := launcher.RunLease(context.Background(), lease)
	if err != nil || result.State != OpenCodeLocalCompleted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if captured.Sandbox.Environment["CODEX_HOME"] != openCodeSandboxHome {
		t.Fatalf("Codex sandbox was not captured: %+v", captured.Sandbox)
	}
	wantPhases := []modelturn.RuntimePhase{modelturn.RuntimePhaseLocalPreflightComplete, modelturn.RuntimePhaseModelAdapterReady, modelturn.RuntimePhaseCodexProcessStarted}
	fixture.remote.mu.Lock()
	phases := append([]modelturn.RuntimePhase(nil), fixture.remote.phases...)
	fixture.remote.mu.Unlock()
	if !slices.Equal(phases, wantPhases) {
		t.Fatalf("phases=%v want=%v", phases, wantPhases)
	}
}

func findSandboxMount(mounts []openCodeSandboxMount, target string) openCodeSandboxMount {
	for _, mount := range mounts {
		if mount.Target == target {
			return mount
		}
	}
	return openCodeSandboxMount{}
}
