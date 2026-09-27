//go:build !windows

package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestSafeProjectToolboxSelectionFailureCategories(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{edgeclient.ErrProjectToolboxEndpointUnavailable, "project_toolbox_endpoint_unavailable"},
		{edgeclient.ErrProjectToolboxOwnershipInspectUnavailable, "project_toolbox_ownership_inspect_unavailable"},
		{edgeclient.ErrProjectToolboxStateInspectUnavailable, "project_toolbox_state_inspect_unavailable"},
		{edgeclient.ErrProjectToolboxStorageInspectUnavailable, "project_toolbox_storage_inspect_unavailable"},
		{edgeclient.ErrProjectToolboxContainerUnavailable, "project_toolbox_container_unavailable"},
		{edgeclient.ErrProjectToolboxIdentityMismatch, "project_toolbox_identity_mismatch"},
		{edgeclient.ErrProjectToolboxMountMismatch, "project_toolbox_mount_mismatch"},
		{edgeclient.ErrProjectToolboxResourceMismatch, "project_toolbox_resource_mismatch"},
		{edgeclient.ErrProjectToolboxEnvironmentMismatch, "project_toolbox_environment_mismatch"},
		{edgeclient.ErrProjectToolboxUnsafeState, "project_toolbox_state_unsafe"},
		{edgeclient.ErrProjectToolboxNotOwned, "project_toolbox_failed"},
		{edgeclient.ErrProjectToolboxUnavailable, "project_toolbox_unavailable"},
	}
	for _, test := range tests {
		if got := safeProjectToolboxSelectionFailure(test.err); got != test.want {
			t.Fatalf("failure %v mapped to %q want %q", test.err, got, test.want)
		}
	}
}

func TestCollectProjectToolboxReportsInspectionStageCodes(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{"ownership inspection", edgeclient.ErrProjectToolboxOwnershipInspectUnavailable, "project_toolbox_ownership_inspect_unavailable"},
		{"state inspection", edgeclient.ErrProjectToolboxStateInspectUnavailable, "project_toolbox_state_inspect_unavailable"},
		{"storage inspection", edgeclient.ErrProjectToolboxStorageInspectUnavailable, "project_toolbox_storage_inspect_unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &fakeProjectToolboxManager{statusErr: test.err}
			_, code := collectProjectToolbox(t.Context(), manager, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
			if code != test.want {
				t.Fatalf("failure code=%q want %q", code, test.want)
			}
		})
	}
}

func TestCollectProjectToolboxReportsMissingContainerFromDirectStatus(t *testing.T) {
	manager := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerMissing}
	_, code := collectProjectToolbox(t.Context(), manager, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
	if code != "project_toolbox_container_missing" {
		t.Fatalf("failure code=%q", code)
	}
}

func TestSelectProjectToolboxManagerRecoversAfterSpecificOwnershipMismatch(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxMountMismatch}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
	if err != nil || got != second {
		t.Fatalf("manager=%T err=%v", got, err)
	}
}

func TestProjectToolboxInspectionStagesPreserveRepairReconcileGate(t *testing.T) {
	ownership := &fakeProjectToolboxManager{statusErr: fmt.Errorf("%w: %w", edgeclient.ErrProjectToolboxOwnershipInspectUnavailable, edgeclient.ErrProjectToolboxContainerUnavailable)}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{ownership}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxRepair})
	if err != nil || got != ownership || ownership.repairCalls != 1 {
		t.Fatalf("ownership candidate manager=%T err=%v reconcileCalls=%d", got, err, ownership.repairCalls)
	}

	state := &fakeProjectToolboxManager{statusErr: fmt.Errorf("%w: %w", edgeclient.ErrProjectToolboxStateInspectUnavailable, edgeclient.ErrProjectToolboxUnavailable)}
	got, err = selectProjectToolboxManager(t.Context(), []projectToolboxOperations{state}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxRepair})
	if got != nil || !errors.Is(err, edgeclient.ErrProjectToolboxUnavailable) || state.repairCalls != 0 {
		t.Fatalf("state failure manager=%T err=%v reconcileCalls=%d", got, err, state.repairCalls)
	}
}
