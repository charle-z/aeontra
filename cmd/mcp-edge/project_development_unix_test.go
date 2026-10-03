//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/development"
	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

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
	if _, code := executeProjectDevelopmentCommandStart(context.Background(), filepath.Join(t.TempDir(), "missing-state"), manager, operation); code != "project_development_reconciliation_required" {
		t.Fatalf("missing effect recovery code=%q", code)
	}
	if platform.starts != 0 {
		t.Fatal("recovery-only operation created an effect")
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
