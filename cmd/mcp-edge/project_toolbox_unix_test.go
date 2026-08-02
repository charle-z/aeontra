//go:build !windows

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

type fakeProjectToolboxManager struct {
	execRequest    edgeclient.ProjectToolboxExecRequest
	serviceRequest edgeclient.ProjectToolboxServiceStartRequest
	removed        bool
}

func (*fakeProjectToolboxManager) Create(context.Context, edgeclient.ProjectToolboxCreateRequest) (edgeclient.ProjectToolboxSnapshot, bool, error) {
	return toolboxFixtureSnapshot(), false, nil
}
func (*fakeProjectToolboxManager) Status(context.Context, edgeclient.ProjectToolboxStatusRequest) (edgeclient.ProjectToolboxSnapshot, error) {
	return toolboxFixtureSnapshot(), nil
}
func (manager *fakeProjectToolboxManager) Exec(_ context.Context, request edgeclient.ProjectToolboxExecRequest) (edgeclient.ProjectToolboxSnapshot, error) {
	manager.execRequest = request
	snapshot := toolboxFixtureSnapshot()
	snapshot.Output = "ok\n"
	return snapshot, nil
}
func (*fakeProjectToolboxManager) Repair(context.Context, edgeclient.ProjectToolboxRepairRequest) (edgeclient.ProjectToolboxSnapshot, error) {
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
