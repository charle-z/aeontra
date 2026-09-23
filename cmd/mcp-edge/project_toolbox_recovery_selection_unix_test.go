//go:build !windows

package main

import (
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestSelectProjectToolboxManagerRepairFindsOwnedContainerOnAlternateEndpoint(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable, repairErr: edgeclient.ErrProjectToolboxContainerMissing}
	second := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxRepair})
	if err != nil || got != second {
		t.Fatalf("manager=%T err=%v", got, err)
	}
	if first.statusCalls != 1 || first.repairCalls != 1 || second.statusCalls != 1 || second.repairCalls != 1 {
		t.Fatalf("first status/repair=%d/%d second=%d/%d", first.statusCalls, first.repairCalls, second.statusCalls, second.repairCalls)
	}
}

func TestSelectProjectToolboxManagerRepairFailsClosedOnLabelledOwnershipMismatch(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable, repairErr: edgeclient.ErrProjectToolboxEnvironmentMismatch}
	second := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxRepair})
	if got != nil || !errors.Is(err, edgeclient.ErrProjectToolboxEnvironmentMismatch) {
		t.Fatalf("manager=%T err=%v", got, err)
	}
	if second.statusCalls != 0 || second.repairCalls != 0 {
		t.Fatalf("mismatch incorrectly fell through to alternate endpoint: status=%d repair=%d", second.statusCalls, second.repairCalls)
	}
}

func TestSafeProjectToolboxSelectionFailureReportsMissingWithoutInternalIdentity(t *testing.T) {
	if got := safeProjectToolboxSelectionFailure(edgeclient.ErrProjectToolboxContainerMissing); got != "project_toolbox_container_missing" {
		t.Fatalf("code=%q", got)
	}
}

func TestSingleEndpointCleanupCanRecoverMissingContainerRecord(t *testing.T) {
	manager := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}
	resolved := toolboxSelectionFixture()
	operation := edge.Operation{Kind: edge.OperationProjectToolboxCleanup}
	selected, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{manager}, resolved, operation)
	if err != nil || selected != manager {
		t.Fatalf("selected=%T err=%v", selected, err)
	}
	result, code := collectProjectToolbox(t.Context(), selected, resolved, operation)
	if code != "" || !result.ToolboxRemoved || result.ToolboxID == "" || result.ToolboxRootFSBytes != 0 || !manager.removed {
		t.Fatalf("code=%q result=%+v request=%+v", code, result, manager.cleanupRequest)
	}
}

func TestMissingContainerCleanupDoesNotGuessAcrossEndpoints(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}
	second := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}
	selected, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxCleanup})
	if selected != nil || !errors.Is(err, edgeclient.ErrProjectToolboxContainerUnavailable) || first.removed || second.removed {
		t.Fatalf("selected=%T err=%v removed=%t/%t", selected, err, first.removed, second.removed)
	}
}
