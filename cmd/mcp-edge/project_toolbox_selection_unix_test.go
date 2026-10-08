//go:build !windows

package main

import (
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func toolboxSelectionFixture() edgeclient.ProjectResolution {
	return edgeclient.ProjectResolution{
		Project:     edgeclient.Project{Alias: "project", Owner: "charle-z", Repository: "repo"},
		TargetAlias: "parrot",
		Workspace: edgeclient.Workspace{
			ID: "ws_22222222222222222222222222222222", Path: "/private/workspace",
			Profile: edgeclient.WorkspaceProfileLinuxWorkcell, Mode: edgeclient.WorkspaceModeDev,
		},
	}
}

func TestSelectProjectToolboxManagerRecoversAlternateRootlessEngine(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxNotOwned}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
	if err != nil || got != second {
		t.Fatalf("manager=%T err=%v", got, err)
	}
	if first.statusCalls != 1 || second.statusCalls != 1 {
		t.Fatalf("status calls first=%d second=%d", first.statusCalls, second.statusCalls)
	}
}

func TestSelectProjectToolboxManagerSkipsUnavailableEndpoint(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxUnavailable}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxExec})
	if err != nil || got != second {
		t.Fatalf("manager=%T err=%v", got, err)
	}
}

func TestSelectProjectToolboxManagerKeepsPreferredEndpointWithoutRecord(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxNotFound}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxCreate})
	if err != nil || got != first {
		t.Fatalf("manager=%T err=%v", got, err)
	}
	if first.statusCalls != 1 || second.statusCalls != 1 {
		t.Fatalf("status calls first=%d second=%d", first.statusCalls, second.statusCalls)
	}
}

func TestSelectProjectToolboxCreateDoesNotInferNoRecordAcrossUnavailableEndpoint(t *testing.T) {
	for _, managers := range [][]*fakeProjectToolboxManager{
		{{statusErr: edgeclient.ErrProjectToolboxNotFound}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}},
		{{statusErr: edgeclient.ErrProjectToolboxUnavailable}, {statusErr: edgeclient.ErrProjectToolboxNotFound}},
	} {
		var candidates []projectToolboxOperations
		for _, manager := range managers {
			candidates = append(candidates, manager)
		}
		selected, err := selectProjectToolboxManager(t.Context(), candidates, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxCreate})
		if selected != nil || !errors.Is(err, edgeclient.ErrProjectToolboxUnavailable) {
			t.Fatalf("selected=%T err=%v; unavailable endpoint must outrank no-record for create", selected, err)
		}
	}
}

func TestSelectProjectToolboxManagerKeepsOfflineHarnessArtifactsIndependent(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxNotOwned}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectBrowserHarnessArtifactRead})
	if err != nil || got != first {
		t.Fatalf("manager=%T err=%v", got, err)
	}
	if first.statusCalls != 0 || second.statusCalls != 0 {
		t.Fatalf("artifact read unexpectedly required live ownership: first=%d second=%d", first.statusCalls, second.statusCalls)
	}
}
