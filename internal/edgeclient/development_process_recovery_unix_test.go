//go:build !windows

package edgeclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryRequestForStart(request ProjectProcessStartRequest) ProjectProcessRecoveryRequest {
	return ProjectProcessRecoveryRequest{
		OperationID: request.OperationID, IdempotencyKey: request.IdempotencyKey,
		ProjectAlias: request.ProjectAlias, TargetAlias: request.TargetAlias,
		ProjectOwner: request.ProjectOwner, ProjectRepository: request.ProjectRepository,
		ProjectClaimGeneration: request.ProjectClaimGeneration, WorkspaceID: request.Workspace.ID,
		Argv: request.Argv, CWD: request.CWD, Stdin: request.Stdin, Environment: request.Environment,
		DevelopmentBindingDigest: request.DevelopmentBindingDigest,
	}
}

func TestDevelopmentProcessOptionalBindingPreservesLegacyDigestBytes(t *testing.T) {
	request := ProjectProcessStartRequest{ProjectAlias: "project", TargetAlias: "parrot", ProjectOwner: "charle-z", ProjectRepository: "repo", ProjectClaimGeneration: 1, ProjectState: "ready",
		Workspace: Workspace{ID: "ws_0123456789abcdef0123456789abcdef"}, Argv: []string{"make", "test"}}
	legacy := `{"WorkspaceID":"ws_0123456789abcdef0123456789abcdef","ProjectAlias":"project","TargetAlias":"parrot","ProjectOwner":"charle-z","ProjectRepository":"repo","ProjectState":"ready","ProjectClaimGeneration":1,"Argv":["make","test"],"CWD":"","Stdin":"","Environment":null}`
	sum := sha256.Sum256([]byte(legacy))
	want := hex.EncodeToString(sum[:])
	got, err := projectProcessRequestDigest(request)
	if err != nil || got != want {
		t.Fatalf("legacy digest got=%q want=%q err=%v", got, want, err)
	}
	request.DevelopmentBindingDigest = "sha256:" + strings.Repeat("a", 64)
	bound, err := projectProcessRequestDigest(request)
	if err != nil || bound == got {
		t.Fatalf("private binding not included digest=%q err=%v", bound, err)
	}
}

func TestDevelopmentProcessRecoveryPreservesCapturedEffectAfterSourceChanges(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "development-recover-one")
	request.ProjectState = "ready"
	request.DevelopmentBindingDigest = "sha256:" + strings.Repeat("a", 64)
	started, created, err := manager.Start(context.Background(), request)
	if err != nil || !created {
		t.Fatalf("start created=%v err=%v", created, err)
	}
	if err := os.WriteFile(filepath.Join(request.Workspace.Path, "generated.go"), []byte("ordinary command output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, found, err := manager.RecoverySnapshotByIdempotency(recoveryRequestForStart(request))
	if err != nil || !found || recovered.ProcessID != started.ProcessID || recovered.ProjectState != "ready" {
		t.Fatalf("recover found=%v snapshot=%+v err=%v", found, recovered, err)
	}
	if len(platform.specs) != 1 {
		t.Fatal("recovery started another effect")
	}
	platform.mu.Lock()
	pid := platform.nextPID
	platform.mu.Unlock()
	platform.naturalExit(pid, 0)
}

func TestDevelopmentProcessRecoveryRejectsChangedEffectAndWorkspaceBinding(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "development-recover-bound")
	request.ProjectState = "ready"
	request.DevelopmentBindingDigest = "sha256:" + strings.Repeat("a", 64)
	if _, _, err := manager.Start(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ProjectProcessRecoveryRequest){
		func(r *ProjectProcessRecoveryRequest) { r.OperationID = "eo_ffffffffffffffffffffffffffffffff" },
		func(r *ProjectProcessRecoveryRequest) { r.WorkspaceID = "ws_ffffffffffffffffffffffffffffffff" },
		func(r *ProjectProcessRecoveryRequest) { r.ProjectAlias = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.TargetAlias = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.ProjectOwner = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.ProjectRepository = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.ProjectClaimGeneration++ },
		func(r *ProjectProcessRecoveryRequest) { r.Argv = []string{"go", "test", "./..."} },
		func(r *ProjectProcessRecoveryRequest) { r.CWD = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.Stdin = "other" },
		func(r *ProjectProcessRecoveryRequest) { r.Environment = map[string]string{"PORT": "9090"} },
		func(r *ProjectProcessRecoveryRequest) {
			r.DevelopmentBindingDigest = "sha256:" + strings.Repeat("b", 64)
		},
	} {
		expected := recoveryRequestForStart(request)
		mutate(&expected)
		if _, found, err := manager.RecoverySnapshotByIdempotency(expected); !errors.Is(err, ErrProjectProcessIdempotencyConflict) || found {
			t.Fatalf("changed binding found=%v err=%v", found, err)
		}
	}
	missing := recoveryRequestForStart(request)
	missing.IdempotencyKey = "development-recover-missing"
	if _, found, err := manager.RecoverySnapshotByIdempotency(missing); err != nil || found {
		t.Fatalf("missing effect found=%v err=%v", found, err)
	}
	if len(platform.specs) != 1 {
		t.Fatal("recovery created an effect")
	}
	platform.mu.Lock()
	pid := platform.nextPID
	platform.mu.Unlock()
	platform.naturalExit(pid, 0)
}

func TestDevelopmentProcessRecoveryDoesNotAdoptReusedPID(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "development-recover-reused-pid")
	request.ProjectState = "ready"
	request.DevelopmentBindingDigest = "sha256:" + strings.Repeat("a", 64)
	started, _, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	platform.mu.Lock()
	pid := platform.nextPID
	platform.processes[pid].identity.StartTicks++
	platform.mu.Unlock()
	recovered, found, err := manager.RecoverySnapshotByIdempotency(recoveryRequestForStart(request))
	if err != nil || !found || recovered.ProcessID != started.ProcessID || recovered.State != ProjectProcessFailed || recovered.ExitKnown || recovered.Reason != "process_identity_changed" {
		t.Fatalf("reused PID recovery found=%v snapshot=%+v err=%v", found, recovered, err)
	}
	if len(platform.specs) != 1 || len(platform.signals) != 0 {
		t.Fatal("recovery affected the replacement process")
	}
	platform.naturalExit(pid, 0)
}

func TestDevelopmentProcessRecoverySurvivesManagerRestart(t *testing.T) {
	platform := newFakeProjectProcessPlatform()
	manager := openTestProjectProcessManager(t, platform, 1<<20)
	request := testProjectProcessRequest(t, "development-recover-restart")
	request.ProjectState = "ready"
	request.DevelopmentBindingDigest = "sha256:" + strings.Repeat("a", 64)
	started, _, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProjectProcessManager(ProjectProcessManagerConfig{StateRoot: manager.stateRoot, Platform: platform})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	recovered, found, err := reopened.RecoverySnapshotByIdempotency(recoveryRequestForStart(request))
	if err != nil || !found || recovered.ProcessID != started.ProcessID || recovered.State != ProjectProcessRunning || len(platform.specs) != 1 {
		t.Fatalf("restart recovery found=%v snapshot=%+v err=%v", found, recovered, err)
	}
	platform.mu.Lock()
	pid := platform.nextPID
	platform.mu.Unlock()
	platform.naturalExit(pid, 0)
}
