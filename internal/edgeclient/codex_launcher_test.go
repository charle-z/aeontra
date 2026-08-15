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
		`web_search="disabled"`,
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
	for key, want := range map[string]string{
		"GIT_AUTHOR_NAME": "MCP Devbox Codex", "GIT_AUTHOR_EMAIL": "codex@mcp-devbox.invalid",
		"GIT_COMMITTER_NAME": "MCP Devbox Codex", "GIT_COMMITTER_EMAIL": "codex@mcp-devbox.invalid",
	} {
		if spec.Sandbox.Environment[key] != want {
			t.Fatalf("%s=%q want=%q", key, spec.Sandbox.Environment[key], want)
		}
	}
	if mount := findSandboxMount(spec.Sandbox.Mounts, codexSandboxExecutable); mount.Source != codexPath || mount.Writable {
		t.Fatalf("Codex executable mount=%+v", mount)
	}
}

func TestCodexLinkedWorktreeSpecMountsExactGitMetadata(t *testing.T) {
	fixture := newProjectWorktreeFixture(t)
	manager, err := OpenProjectWorktreeManager(ProjectWorktreeManagerConfig{
		StateRoot: fixture.stateRoot, Roots: fixture.roots, Workspaces: fixture.workspaces,
		Runner:     NewDevGitCommandRunner(fixture.stateRoot, "/usr/local/bin:/usr/bin:/bin"),
		Credential: GitHubCredential{SchemaVersion: 1, Owner: "charle-z", Token: "gho_" + strings.Repeat("c", 36)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	created, _, err := manager.Create(context.Background(), ProjectWorktreeCreateRequest{
		Alias: "project", TargetAlias: "parrot", Repository: "charle-z/project",
		CanonicalWorkspaceID: fixture.canonical.ID, CanonicalPath: fixture.canonical.Path,
		BaseCommit: fixture.head, Role: ProjectWorktreeWriter,
		JobID: "wj_cccccccccccccccccccccccccccccccc", LeaseID: "wl_cccccccccccccccccccccccccccccccc", Fence: 1,
		IdempotencyKey: "worktree-codex-git-metadata",
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := fixture.workspaces.Get(created.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pinPath := filepath.Join(t.TempDir(), "pin.json")
	if err := os.WriteFile(pinPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bubblewrap := filepath.Join(t.TempDir(), "bwrap")
	if err := os.WriteFile(bubblewrap, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal, err := OpenOpenCodeRuntimeJournal(fixture.stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	launcher, err := NewCodexLauncher(CodexLauncherConfig{
		StateRoot: fixture.stateRoot, CodexPath: codexPath, CodexPinPath: pinPath,
		BubblewrapPath: bubblewrap, OutputLimit: 4096, Workspaces: fixture.workspaces, Journal: journal,
	})
	if err != nil {
		t.Fatal(err)
	}
	lease := ModelRuntimeLease{
		RuntimeID: "mr_cccccccccccccccccccccccccccccccc", DeviceID: "ed_cccccccccccccccccccccccccccccccc",
		WorkspaceID: workspace.ID, Controller: modelturn.ControllerRemoteEdge, State: modelturn.RuntimeStateStarting,
		Goal: "commit the isolated fixture", GoalDigest: "sha256:" + strings.Repeat("c", 64), TimeoutSeconds: 60, ProviderProfile: remoteProviderProfile,
	}
	runtimeDir := filepath.Join(fixture.stateRoot, "r", lease.RuntimeID)
	spec, err := launcher.codexLinuxWorkcellProcessSpec(runtimeDir, workspace, LinuxWorkcellPreparation{Workspace: workspace}, "http://127.0.0.1:43210/v1", lease, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	gitPointer, err := os.ReadFile(filepath.Join(created.path, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Clean(strings.TrimSpace(strings.TrimPrefix(string(gitPointer), "gitdir: ")))
	for key, want := range map[string]string{
		"GIT_DIR": filepath.ToSlash(filepath.Join(codexSandboxGitCommon, "worktrees", filepath.Base(gitDir))), "GIT_WORK_TREE": openCodeSandboxWorkspace,
	} {
		if spec.Sandbox.Environment[key] != want {
			t.Fatalf("%s=%q want=%q", key, spec.Sandbox.Environment[key], want)
		}
	}
	if got := spec.Sandbox.Environment["GIT_COMMON_DIR"]; got != "" {
		t.Fatalf("GIT_COMMON_DIR=%q want empty so the linked gitdir resolves commondir itself", got)
	}
	mount := findSandboxMount(spec.Sandbox.Mounts, codexSandboxGitCommon)
	wantSource := filepath.Join(fixture.canonical.Path, ".git")
	if mount.Source != wantSource || !mount.Writable || mount.Kind != "bind" {
		t.Fatalf("Git common metadata mount=%+v want source=%s writable bind", mount, wantSource)
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
