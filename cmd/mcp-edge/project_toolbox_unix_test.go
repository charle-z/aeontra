//go:build !windows

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type fakeProjectToolboxManager struct {
	createRequest  edgeclient.ProjectToolboxCreateRequest
	execRequest    edgeclient.ProjectToolboxExecRequest
	serviceRequest edgeclient.ProjectToolboxServiceStartRequest
	removed        bool
	statusErr      error
	statusCalls    int
	repairErr      error
	repairCalls    int
}

func (manager *fakeProjectToolboxManager) Create(_ context.Context, request edgeclient.ProjectToolboxCreateRequest) (edgeclient.ProjectToolboxSnapshot, bool, error) {
	manager.createRequest = request
	return toolboxFixtureSnapshot(), false, nil
}
func (manager *fakeProjectToolboxManager) Status(context.Context, edgeclient.ProjectToolboxStatusRequest) (edgeclient.ProjectToolboxSnapshot, error) {
	manager.statusCalls++
	if manager.statusErr != nil {
		return edgeclient.ProjectToolboxSnapshot{}, manager.statusErr
	}
	return toolboxFixtureSnapshot(), nil
}
func (manager *fakeProjectToolboxManager) Exec(_ context.Context, request edgeclient.ProjectToolboxExecRequest) (edgeclient.ProjectToolboxSnapshot, error) {
	manager.execRequest = request
	snapshot := toolboxFixtureSnapshot()
	snapshot.Output = "ok\n"
	return snapshot, nil
}
func (manager *fakeProjectToolboxManager) Repair(context.Context, edgeclient.ProjectToolboxRepairRequest) (edgeclient.ProjectToolboxSnapshot, error) {
	manager.repairCalls++
	if manager.repairErr != nil {
		return edgeclient.ProjectToolboxSnapshot{}, manager.repairErr
	}
	return toolboxFixtureSnapshot(), nil
}
func (manager *fakeProjectToolboxManager) ServiceStart(_ context.Context, request edgeclient.ProjectToolboxServiceStartRequest) (edgeclient.ProjectToolboxServiceSnapshot, bool, error) {
	manager.serviceRequest = request
	return toolboxFixtureService(), false, nil
}
func (*fakeProjectToolboxManager) ServiceStatus(context.Context, edgeclient.ProjectToolboxServiceRequest) (edgeclient.ProjectToolboxServiceSnapshot, error) {
	return toolboxFixtureService(), nil
}
func (*fakeProjectToolboxManager) ServiceStop(context.Context, edgeclient.ProjectToolboxServiceRequest) (edgeclient.ProjectToolboxServiceSnapshot, error) {
	service := toolboxFixtureService()
	service.State = "stopped"
	return service, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessStart(context.Context, edgeclient.ProjectBrowserHarnessStartRequest) (edgeclient.ProjectBrowserHarnessSnapshot, bool, error) {
	return browserHarnessFixtureSnapshot(), false, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessStatus(context.Context, edgeclient.ProjectBrowserHarnessStatusRequest) (edgeclient.ProjectBrowserHarnessSnapshot, error) {
	return browserHarnessFixtureSnapshot(), nil
}
func (*fakeProjectToolboxManager) BrowserHarnessList(context.Context, edgeclient.ProjectBrowserHarnessListRequest) ([]edgeclient.ProjectBrowserHarnessSummary, error) {
	snapshot := browserHarnessFixtureSnapshot()
	return []edgeclient.ProjectBrowserHarnessSummary{{RunID: snapshot.RunID, State: snapshot.State, Profile: snapshot.Profile, CreatedAt: snapshot.CreatedAt, UpdatedAt: snapshot.UpdatedAt, TimeoutSeconds: snapshot.TimeoutSeconds, StorageMiB: snapshot.StorageMiB}}, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessStop(context.Context, edgeclient.ProjectBrowserHarnessStopRequest) (edgeclient.ProjectBrowserHarnessSnapshot, error) {
	snapshot := browserHarnessFixtureSnapshot()
	snapshot.State = "stopped"
	return snapshot, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessCleanup(edgeclient.ProjectBrowserHarnessCleanupRequest) (edgeclient.ProjectBrowserHarnessCleanupResult, error) {
	return edgeclient.ProjectBrowserHarnessCleanupResult{Runs: 1, Artifacts: 1, Profiles: 1}, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessArtifactList(edgeclient.ProjectBrowserHarnessArtifactListRequest) ([]edgeclient.ProjectBrowserHarnessArtifactSummary, error) {
	return []edgeclient.ProjectBrowserHarnessArtifactSummary{{Path: "artifacts/trace.zip", MediaType: "application/zip", Bytes: 10, SHA256: strings.Repeat("a", 64), UpdatedAt: time.Date(2026, 8, 4, 4, 0, 1, 0, time.UTC)}}, nil
}
func (*fakeProjectToolboxManager) BrowserHarnessArtifactRead(edgeclient.ProjectBrowserHarnessArtifactReadRequest) (edgeclient.ProjectBrowserHarnessArtifactChunk, error) {
	return edgeclient.ProjectBrowserHarnessArtifactChunk{RunID: "bh_44444444444444444444444444444444", Path: "artifacts/trace.zip", MediaType: "application/zip", Bytes: 10, SHA256: strings.Repeat("a", 64), Next: 10, EOF: true, DataBase64: "dHJhY2UtYm9keQ=="}, nil
}

func browserHarnessFixtureSnapshot() edgeclient.ProjectBrowserHarnessSnapshot {
	return edgeclient.ProjectBrowserHarnessSnapshot{RunID: "bh_44444444444444444444444444444444", State: "running", Profile: "default", CreatedAt: time.Date(2026, 8, 4, 4, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 4, 4, 0, 1, 0, time.UTC), TimeoutSeconds: 3600, StorageMiB: 2048, StdoutEOF: true, StderrEOF: true}
}

func (manager *fakeProjectToolboxManager) Cleanup(context.Context, edgeclient.ProjectToolboxCleanupRequest) (bool, error) {
	manager.removed = true
	return true, nil
}

func toolboxFixtureService() edgeclient.ProjectToolboxServiceSnapshot {
	return edgeclient.ProjectToolboxServiceSnapshot{
		ServiceID: "ts_33333333333333333333333333333333", Name: "preview", State: "running",
		CreatedAt: time.Date(2026, 8, 2, 12, 2, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 2, 12, 3, 0, 0, time.UTC),
	}
}

func toolboxFixtureSnapshot() edgeclient.ProjectToolboxSnapshot {
	return edgeclient.ProjectToolboxSnapshot{
		ToolboxID: "tb_11111111111111111111111111111111", State: edgeclient.ProjectToolboxRunning,
		BaseImage: "docker.io/library/debian:bookworm-slim", BaseImageID: "sha256:" + strings.Repeat("a", 64),
		CreatedAt: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 2, 12, 1, 0, 0, time.UTC),
		CPUMillis: 4000, MemoryMiB: 8192, ProcessLimit: 2048,
		ContainerAccess: false, WritableBytes: 4096, RootFSBytes: 80 << 20,
	}
}

func TestCollectProjectToolboxMapsCreateResourceLimits(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project: edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"}, TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{ID: "ws_22222222222222222222222222222222", Path: "/private/workspace", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
	}
	manager := &fakeProjectToolboxManager{}
	operation := edge.Operation{Kind: edge.OperationProjectToolboxCreate, Request: edge.OperationRequest{ToolboxCPUMillis: 4000, ToolboxMemoryMiB: 8192, ToolboxProcessLimit: 2048}}
	result, code := collectProjectToolbox(t.Context(), manager, resolved, operation)
	if code != "" || manager.createRequest.CPUMillis != 4000 || manager.createRequest.MemoryMiB != 8192 || manager.createRequest.ProcessLimit != 2048 {
		t.Fatalf("request=%+v result=%+v code=%q", manager.createRequest, result, code)
	}
	if result.ToolboxCPUMillis != 4000 || result.ToolboxMemoryMiB != 8192 || result.ToolboxProcessLimit != 2048 {
		t.Fatalf("result=%+v", result)
	}
	if result.ToolboxContainerAccess || result.ToolboxWritableBytes != 4096 || result.ToolboxRootFSBytes != 80<<20 {
		t.Fatalf("container metadata=%+v", result)
	}
}

func TestCollectProjectToolboxMapsOpaqueServiceIdentity(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project: edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"}, TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{ID: "ws_22222222222222222222222222222222", Path: "/private/workspace", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
	}
	manager := &fakeProjectToolboxManager{}
	operation := edge.Operation{Kind: edge.OperationProjectToolboxServiceStart, Request: edge.OperationRequest{ToolboxServiceName: "preview", Argv: []string{"go", "run", "./cmd/demo"}}}
	result, code := collectProjectToolbox(t.Context(), manager, resolved, operation)
	if code != "" || result.ToolboxServiceID != "ts_33333333333333333333333333333333" || result.ToolboxServiceName != "preview" || result.ToolboxServiceState != "running" {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	if manager.serviceRequest.Name != "preview" || manager.serviceRequest.Argv[0] != "go" || strings.Contains(result.ToolboxServiceID, "pid") {
		t.Fatalf("request=%+v result=%+v", manager.serviceRequest, result)
	}
}

func TestCollectProjectToolboxMapsExecAndCleanupWithoutInternalIdentity(t *testing.T) {
	resolved := edgeclient.ProjectResolution{
		Project: edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"}, TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{ID: "ws_22222222222222222222222222222222", Path: "/private/workspace", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev},
	}
	manager := &fakeProjectToolboxManager{}
	operation := edge.Operation{Kind: edge.OperationProjectToolboxExec, Request: edge.OperationRequest{Argv: []string{"ruby", "--version"}, CWD: "src", Environment: map[string]string{"CI": "true"}, TimeoutSeconds: 60}}
	result, code := collectProjectToolbox(t.Context(), manager, resolved, operation)
	if code != "" || result.ToolboxID == "" || result.ToolboxState != "running" || result.ToolboxOutput != "ok\n" || result.WorkspaceID != resolved.Workspace.ID {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	if strings.Contains(result.ToolboxBase, "/") || manager.execRequest.CWD != "src" || manager.execRequest.Argv[0] != "ruby" {
		t.Fatalf("public base=%q request=%+v", result.ToolboxBase, manager.execRequest)
	}
	operation.Kind = edge.OperationProjectToolboxCleanup
	result, code = collectProjectToolbox(t.Context(), manager, resolved, operation)
	if code != "" || !manager.removed || !result.ToolboxRemoved || result.ToolboxState != "removed" {
		t.Fatalf("cleanup result=%+v code=%q", result, code)
	}
}

func TestCollectProjectBrowserHarnessMapsOnlySafeLifecycleMetadata(t *testing.T) {
	resolved := edgeclient.ProjectResolution{Project: edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"}, TargetAlias: "parrot", Workspace: edgeclient.Workspace{ID: "ws_22222222222222222222222222222222", Path: "/private/workspace", Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev}}
	manager := &fakeProjectToolboxManager{}
	operation := edge.Operation{Kind: edge.OperationProjectBrowserHarnessStart, Request: edge.OperationRequest{IdempotencyKey: "harness-1", Argv: []string{"node", "secret-script.mjs"}, BrowserHarnessProfile: "default", BrowserHarnessTimeoutSeconds: 3600, BrowserHarnessStorageMiB: 2048}}
	result, code := collectProjectBrowserHarness(t.Context(), manager, resolved, operation)
	if code != "" || result.BrowserHarnessRunID != "bh_44444444444444444444444444444444" || result.BrowserHarnessState != "running" || result.BrowserHarnessProfile != "default" || result.BrowserHarnessTimeoutSeconds != 3600 || result.BrowserHarnessStorageMiB != 2048 {
		t.Fatalf("result=%+v code=%q", result, code)
	}
	encoded := fmt.Sprintf("%+v", result)
	for _, forbidden := range []string{"secret-script.mjs", "/private/workspace", "node"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("result leaked %q: %s", forbidden, encoded)
		}
	}
}
