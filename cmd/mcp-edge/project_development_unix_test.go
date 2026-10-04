//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestProjectDevelopmentGitRunnerReadsRealSourceWithoutAmbientPATH(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("real Git is required for development source evidence")
	}
	stores, workspace, _ := newProjectCommandFixture(t)
	t.Cleanup(func() {
		_ = stores.projects.Close()
		_ = stores.workspaces.Close()
	})
	gitHome := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), gitPath, append([]string{
			"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test",
		}, args...)...)
		command.Dir = workspace.Path
		command.Env = []string{"HOME=" + gitHome, "PATH=/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0"}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git %q: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--quiet")
	readme := filepath.Join(workspace.Path, "README.md")
	if err := os.WriteFile(readme, []byte("development source fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "--quiet", "-m", "test: initialize source fixture")
	expectedHead := git("rev-parse", "HEAD")
	resolved, err := stores.projects.Resolve(t.Context(), "ekoparty", "parrot")
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the production factory with no usable caller PATH. The runner
	// must discover trusted system Git, not rely on the fixture's setup PATH.
	t.Setenv("PATH", t.TempDir())
	runner := projectDevelopmentGitRunner(t.TempDir())
	digest, head, clean, err := edgeclient.RegisteredProjectSourceEvidence(t.Context(), stores.projects, resolved, runner)
	if err != nil || !strings.HasPrefix(digest, "sha256:") || head != expectedHead || !clean {
		t.Fatalf("clean source digest=%q head=%q clean=%v err=%v", digest, head, clean, err)
	}
	if err := os.WriteFile(readme, []byte("ordinary development edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirtyDigest, dirtyHead, dirtyClean, err := edgeclient.RegisteredProjectSourceEvidence(t.Context(), stores.projects, resolved, runner)
	if err != nil || dirtyDigest == digest || dirtyHead != expectedHead || dirtyClean {
		t.Fatalf("dirty source digest=%q head=%q clean=%v err=%v", dirtyDigest, dirtyHead, dirtyClean, err)
	}
}

func TestDevelopmentCommandTimeoutUsesSystemExecutable(t *testing.T) {
	operation := edge.Operation{Request: edge.OperationRequest{
		Argv:               []string{"true"},
		DevelopmentCommand: &edge.ProjectDevelopmentCommandBinding{TimeoutSeconds: 60},
	}}
	process := developmentCommandProcessOperation(operation)
	if process.Request.Argv[0] != "/usr/bin/timeout" {
		t.Fatalf("deadline wrapper must not resolve through writable runtime PATH: %q", process.Request.Argv[0])
	}
}

func TestDevelopmentInventoryFailureKeepsMeasurementDistinct(t *testing.T) {
	if code := safeDevelopmentInventoryFailure(errors.Join(errors.New("bounded probe observation"), edgeclient.ErrDevelopmentWorkcellInventoryMeasurementFailed)); code != "project_development_inventory_measurement_failed" {
		t.Fatalf("measurement failure code=%q", code)
	}
	if code := safeDevelopmentInventoryFailure(errors.New("journal unavailable")); code != "project_development_inventory_unavailable" {
		t.Fatalf("inventory unavailable code=%q", code)
	}
}

func TestProjectDevelopmentProducerOptionalGoEvidenceKeepsGenericInspection(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("real Git is required for producer source binding")
	}
	for _, kind := range []string{"malformed", "oversized", "symlink", "ignored symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			stateRoot := t.TempDir()
			roots, err := edgeclient.DefaultWorkspaceRoots()
			if err != nil {
				t.Fatal(err)
			}
			workspacePath := filepath.Join(roots.Dev, "producer-fixture")
			if err := os.MkdirAll(workspacePath, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(roots.HTBLinux, 0o700); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) {
				t.Helper()
				command := exec.CommandContext(t.Context(), gitPath, append([]string{"-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
				command.Dir = workspacePath
				command.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/local/bin:/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_TERMINAL_PROMPT=0"}
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("fixture Git: %v: %s", err, output)
				}
			}
			git("init", "--quiet")
			git("remote", "add", "origin", "https://github.com/charle-z/producer-fixture.git")
			for name, content := range map[string]string{"go.mod": "module example.test/project\ngo 1.26.6\n", "Makefile": "all:\n\ttrue\n"} {
				if err := os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workPath := filepath.Join(workspacePath, "go.work")
			switch kind {
			case "malformed":
				err = os.WriteFile(workPath, []byte("go invalid\n"), 0o600)
			case "oversized":
				err = os.WriteFile(workPath, []byte(strings.Repeat(" ", (128<<10)+1)), 0o600)
			case "symlink", "ignored symlink":
				err = os.Symlink(filepath.Join(t.TempDir(), "outside-workspace"), workPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			git("add", "go.mod", "Makefile")
			if kind == "ignored symlink" {
				if err := os.WriteFile(filepath.Join(workspacePath, ".gitignore"), []byte("go.work\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				git("add", ".gitignore")
			} else {
				git("add", "go.work")
			}
			git("commit", "--quiet", "-m", "test: initialize producer fixture")
			if _, err := edgeclient.ConfigureGitHubCredential(stateRoot, "charle-z", strings.NewReader("fixture-only-authority-not-a-real-token")); err != nil {
				t.Fatal(err)
			}
			workspaces, err := edgeclient.OpenWorkspaceRegistryWithRoots(stateRoot, roots)
			if err != nil {
				t.Fatal(err)
			}
			workspace, _, err := workspaces.AddProfile(workspacePath, edgeclient.WorkspaceProfileLinuxWorkcell)
			if err != nil {
				t.Fatal(err)
			}
			projects, err := edgeclient.OpenProjectRegistry(edgeclient.ProjectRegistryConfig{StateRoot: stateRoot, AllowedOwner: "charle-z", Workspaces: workspaces})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := projects.Register(edgeclient.ProjectRegistration{Alias: "project", Owner: "charle-z", Repository: "producer-fixture", PreferredTarget: "parrot", TargetAlias: "parrot", WorkspaceID: workspace.ID, AllowedProfiles: []edgeclient.WorkspaceProfile{edgeclient.WorkspaceProfileLinuxWorkcell}}); err != nil {
				t.Fatal(err)
			}
			if err := projects.Close(); err != nil {
				t.Fatal(err)
			}
			if err := workspaces.Close(); err != nil {
				t.Fatal(err)
			}
			inspection, code := inspectProjectDevelopmentWithInventory(t.Context(), stateRoot, edge.Operation{ID: "eo_" + strings.Repeat("a", 32), Request: edge.OperationRequest{Alias: "project", TargetAlias: "parrot"}},
				func(context.Context, edgeclient.DirectWorkcellCommandRequest) ([]edgeclient.LinuxToolInventoryEntry, error) {
					return []edgeclient.LinuxToolInventoryEntry{{Name: "go", Available: true, Version: "1.26.6", Capability: "go-toolchain"}, {Name: "make", Available: true, Version: "4.4", Capability: "build-tool"}}, nil
				})
			if code != "" || inspection == nil {
				t.Fatalf("optional Go metadata blocked generic producer: %s", code)
			}
			observed := inspection.result.DevelopmentInspection
			if observed.GoCommandRequirements != nil || !observed.SourceClean || !reflect.DeepEqual(observed.Requirements, []development.CapabilityID{"build.make", "toolchain.go.v1-26-6"}) {
				t.Fatalf("generic source requirements changed: %+v", observed)
			}
		})
	}
}

type developmentTestProcessPlatform struct {
	starts int
	exits  chan edgeclient.ProjectProcessExit
}

func (p *developmentTestProcessPlatform) Start(spec edgeclient.DirectWorkcellProcessSpec) (edgeclient.ProjectProcessIdentity, <-chan edgeclient.ProjectProcessExit, error) {
	p.starts++
	p.exits = make(chan edgeclient.ProjectProcessExit, 1)
	return edgeclient.ProjectProcessIdentity{ProcessID: spec.PersistentProcessID, PID: 4000, ProcessGroupID: 4000, StartTicks: 40}, p.exits, nil
}

func (*developmentTestProcessPlatform) Alive(edgeclient.ProjectProcessIdentity) (bool, error) {
	return true, nil
}

func (*developmentTestProcessPlatform) Signal(edgeclient.ProjectProcessIdentity, edgeclient.ProjectProcessSignal) error {
	return nil
}

func (*developmentTestProcessPlatform) WriteStdin(edgeclient.ProjectProcessIdentity, edgeclient.ProjectProcessStdinWrite) (edgeclient.ProjectProcessStdinReceipt, error) {
	return edgeclient.ProjectProcessStdinReceipt{}, nil
}

func developmentHandlerFixture(t *testing.T) (*edgeclient.ProjectProcessManager, *developmentTestProcessPlatform, edge.Operation, edgeclient.ProjectResolution) {
	t.Helper()
	platform := &developmentTestProcessPlatform{}
	manager, err := edgeclient.OpenProjectProcessManager(edgeclient.ProjectProcessManagerConfig{StateRoot: t.TempDir(), Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if platform.exits != nil {
			platform.exits <- edgeclient.ProjectProcessExit{ExitKnown: true, ExitCode: 0}
		}
		_ = manager.Close()
	})
	anchor := development.WorkspaceAnchor{DeviceID: "ed_" + strings.Repeat("a", 32), WorkspaceID: "ws_" + strings.Repeat("b", 32), Generation: 1, Owner: "charle-z", Repository: "project"}
	resolved := edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: anchor.Owner, Repository: anchor.Repository, ClaimGeneration: anchor.Generation, ClaimGenerationValid: true},
		TargetAlias: "parrot", Workspace: edgeclient.Workspace{ID: anchor.WorkspaceID, Path: t.TempDir(), Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
		CheckoutState: edgeclient.ProjectCheckoutReady,
	}
	argv := []string{"make", "test"}
	digest, err := development.CommandDigest(argv)
	if err != nil {
		t.Fatal(err)
	}
	op := edge.Operation{ID: "eo_" + strings.Repeat("c", 32), DeviceID: anchor.DeviceID, Kind: edge.OperationProjectDevelopmentCommandStart,
		Request: edge.OperationRequest{Alias: "project", TargetAlias: "parrot", Profile: "linux-workcell", IdempotencyKey: "development-handler-recovery", Argv: argv,
			DevelopmentCommand: &edge.ProjectDevelopmentCommandBinding{Version: 1, Anchor: anchor, SourceDigest: "sha256:" + strings.Repeat("d", 64), EnvironmentDigest: "sha256:" + strings.Repeat("e", 64), CommandDigest: digest,
				PrivateBodyRef: "mb_" + strings.Repeat("f", 32), PrivateBodyDigest: "sha256:" + strings.Repeat("a", 64), TimeoutSeconds: 60}}}
	return manager, platform, op, resolved
}

func TestDevelopmentCommandRecoversLostACKBeforeMutableInspection(t *testing.T) {
	manager, platform, operation, resolved := developmentHandlerFixture(t)
	processOperation := developmentCommandProcessOperation(operation)
	started, code := startResolvedDevelopmentProcess(context.Background(), manager, processOperation, resolved, edgeclient.WorkspaceRoots{}, operation.Request.DevelopmentCommand.Anchor)
	if code != "" {
		t.Fatal(code)
	}
	if err := os.WriteFile(filepath.Join(resolved.Workspace.Path, "generated.go"), []byte("normal command mutation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The captured effect remains recoverable even after the live registry is
	// unavailable or reassociated; recovery must not inspect it or spawn again.
	recovered, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, operation)
	if code != "" || recovered.BackgroundProcessID != started.BackgroundProcessID || recovered.DevelopmentCommand == nil {
		t.Fatalf("lost ACK recovery code=%q result=%+v", code, recovered)
	}
	if platform.starts != 1 {
		t.Fatal("lost ACK created a second effect")
	}
}

func TestDevelopmentProcessRejectsReassociatedWorkspaceBeforeNewEffect(t *testing.T) {
	manager, platform, operation, resolved := developmentHandlerFixture(t)
	resolved.Workspace.ID = "ws_" + strings.Repeat("a", 32)
	resolved.Project.ClaimGeneration++
	if _, code := startResolvedDevelopmentProcess(context.Background(), manager, developmentCommandProcessOperation(operation), resolved, edgeclient.WorkspaceRoots{}, operation.Request.DevelopmentCommand.Anchor); code != "project_development_identity_mismatch" {
		t.Fatalf("reassociated workspace code=%q", code)
	}
	if platform.starts != 0 {
		t.Fatal("reassociated workspace received a new effect")
	}
}

func TestDevelopmentCommandRecoveryOnlyCannotCreateMissingEffect(t *testing.T) {
	manager, platform, operation, _ := developmentHandlerFixture(t)
	operation.Request.DevelopmentRecoveryOperationID = "eo_" + strings.Repeat("d", 32)
	operation.Request.DevelopmentRecoveryIdempotencyKey = "development-original-missing"
	result, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, operation)
	if code != edge.DevelopmentCommandEffectAbsentSafeCode {
		t.Fatalf("missing effect recovery code=%q", code)
	}
	receipt := result.DevelopmentCommandAbsence
	if receipt == nil || receipt.Version != 1 || receipt.OriginalOperationID != operation.Request.DevelopmentRecoveryOperationID ||
		receipt.OriginalIdempotencyKey != operation.Request.DevelopmentRecoveryIdempotencyKey ||
		!reflect.DeepEqual(receipt.Command, *operation.Request.DevelopmentCommand) || result.BackgroundProcessID != "" || result.BackgroundExitKnown {
		t.Fatalf("missing effect recovery lacked exact absence receipt: %+v", result)
	}
	if platform.starts != 0 {
		t.Fatal("recovery-only operation created an effect")
	}
}

func TestDevelopmentCommandRecoveryUnavailableJournalCannotProveAbsence(t *testing.T) {
	manager, platform, operation, _ := developmentHandlerFixture(t)
	operation.Request.DevelopmentRecoveryOperationID = "eo_" + strings.Repeat("d", 32)
	operation.Request.DevelopmentRecoveryIdempotencyKey = "development-unavailable-journal"
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	result, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, operation)
	if code == "" || code == edge.DevelopmentCommandEffectAbsentSafeCode || result.DevelopmentCommandAbsence != nil || platform.starts != 0 {
		t.Fatalf("unavailable journal fabricated absence: code=%q result=%+v starts=%d", code, result, platform.starts)
	}
}

func TestDevelopmentCommandAbsencePreservesEmptyRequirementBinding(t *testing.T) {
	manager, platform, operation, _ := developmentHandlerFixture(t)
	operation.Request.DevelopmentCommand.Requirements = []development.CapabilityID{}
	operation.Request.DevelopmentRecoveryOperationID = "eo_" + strings.Repeat("d", 32)
	operation.Request.DevelopmentRecoveryIdempotencyKey = "development-original-empty"
	result, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, operation)
	if code != edge.DevelopmentCommandEffectAbsentSafeCode || result.DevelopmentCommandAbsence == nil ||
		!reflect.DeepEqual(result.DevelopmentCommandAbsence.Command, *operation.Request.DevelopmentCommand) || platform.starts != 0 {
		t.Fatalf("absence changed exact empty requirement binding: code=%q result=%+v", code, result)
	}
}

func TestDevelopmentCommandRecoveryOnlyRequiresOriginalPrivateBinding(t *testing.T) {
	manager, platform, operation, resolved := developmentHandlerFixture(t)
	started, code := startResolvedDevelopmentProcess(context.Background(), manager, developmentCommandProcessOperation(operation), resolved, edgeclient.WorkspaceRoots{}, operation.Request.DevelopmentCommand.Anchor)
	if code != "" {
		t.Fatal(code)
	}
	recovery := operation
	recovery.ID = "eo_" + strings.Repeat("d", 32)
	recovery.Request.IdempotencyKey = "development-recovery-journal"
	recovery.Request.DevelopmentRecoveryOperationID = operation.ID
	recovery.Request.DevelopmentRecoveryIdempotencyKey = operation.Request.IdempotencyKey
	result, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, recovery)
	if code != "" || result.BackgroundProcessID != started.BackgroundProcessID {
		t.Fatalf("exact recovery code=%q process=%q", code, result.BackgroundProcessID)
	}
	for _, mutate := range []func(*edge.ProjectDevelopmentCommandBinding){
		func(b *edge.ProjectDevelopmentCommandBinding) { b.SourceDigest = "sha256:" + strings.Repeat("b", 64) },
		func(b *edge.ProjectDevelopmentCommandBinding) {
			b.EnvironmentDigest = "sha256:" + strings.Repeat("b", 64)
		},
		func(b *edge.ProjectDevelopmentCommandBinding) {
			b.PrivateBodyDigest = "sha256:" + strings.Repeat("b", 64)
		},
		func(b *edge.ProjectDevelopmentCommandBinding) { b.TimeoutSeconds++ },
	} {
		forged := recovery
		binding := *recovery.Request.DevelopmentCommand
		mutate(&binding)
		forged.Request.DevelopmentCommand = &binding
		if _, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, forged); code != "project_process_idempotency_conflict" {
			t.Fatalf("changed private binding recovery code=%q", code)
		}
	}
	if platform.starts != 1 {
		t.Fatal("private binding recovery duplicated an effect")
	}
}

func TestDevelopmentBootstrapRecoveryOnlyPreservesExactSelectionAndEffect(t *testing.T) {
	manager, platform, operation, resolved := developmentHandlerFixture(t)
	anchor := operation.Request.DevelopmentCommand.Anchor
	operation.Kind = edge.OperationProjectDevelopmentBootstrapStart
	operation.Request.DevelopmentCommand = nil
	operation.Request.Argv = nil
	selection := development.BootstrapResolution{CapabilityID: "toolchain.go.v1-26", Toolchain: "go", Version: "1.26.6", Platform: runtime.GOARCH,
		ArtifactFile: "go1.26.6.linux-" + runtime.GOARCH + ".tar.gz", ArtifactSHA256: strings.Repeat("a", 64), ArtifactSize: 1024}
	digest, err := development.BootstrapResolutionDigest(selection)
	if err != nil {
		t.Fatal(err)
	}
	operation.Request.DevelopmentBootstrap = &edge.ProjectDevelopmentBootstrapBinding{Version: 1, Anchor: anchor, CapabilityID: selection.CapabilityID, Resolution: &selection, ResolutionDigest: digest}
	recipe, err := edgeclient.BuildDevelopmentBootstrapRecipe(selection)
	if err != nil {
		t.Fatal(err)
	}
	processOperation := operation
	processOperation.Request.Argv, processOperation.Request.Environment = recipe.Argv, recipe.Environment
	started, code := startResolvedDevelopmentProcess(context.Background(), manager, processOperation, resolved, edgeclient.WorkspaceRoots{}, anchor)
	if code != "" {
		t.Fatal(code)
	}
	recovery := operation
	recovery.ID = "eo_" + strings.Repeat("d", 32)
	recovery.Request.IdempotencyKey = "development-bootstrap-recovery"
	recovery.Request.DevelopmentRecoveryOperationID = operation.ID
	recovery.Request.DevelopmentRecoveryIdempotencyKey = operation.Request.IdempotencyKey
	result, code := executeProjectDevelopmentBootstrap(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, recovery)
	if code != "" || result.BackgroundProcessID != started.BackgroundProcessID || !edge.BootstrapBindingsEqual(result.DevelopmentBootstrap, operation.Request.DevelopmentBootstrap) {
		t.Fatalf("bootstrap recovery code=%q process=%q", code, result.BackgroundProcessID)
	}
	changedSelection := selection
	changedSelection.ArtifactSHA256 = strings.Repeat("b", 64)
	changedDigest, err := development.BootstrapResolutionDigest(changedSelection)
	if err != nil {
		t.Fatal(err)
	}
	changedBinding := *operation.Request.DevelopmentBootstrap
	changedBinding.Resolution, changedBinding.ResolutionDigest = &changedSelection, changedDigest
	recovery.Request.DevelopmentBootstrap = &changedBinding
	if _, code := executeProjectDevelopmentBootstrap(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, recovery); code != "project_process_idempotency_conflict" {
		t.Fatalf("changed bootstrap selection code=%q", code)
	}
	if platform.starts != 1 {
		t.Fatal("bootstrap recovery duplicated an effect")
	}
}
