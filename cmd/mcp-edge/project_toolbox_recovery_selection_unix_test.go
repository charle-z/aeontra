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

func TestProjectToolboxRepairDoesNotTreatOneEndpointAbsenceAsGlobalWhenAnotherIsUnavailable(t *testing.T) {
	for _, managers := range [][]*fakeProjectToolboxManager{
		{{statusErr: edgeclient.ErrProjectToolboxContainerMissing}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}},
		{{statusErr: edgeclient.ErrProjectToolboxUnavailable}, {statusErr: edgeclient.ErrProjectToolboxContainerMissing}},
		{{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable, repairErr: edgeclient.ErrProjectToolboxContainerMissing}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}},
	} {
		var candidates []projectToolboxOperations
		for _, manager := range managers {
			candidates = append(candidates, manager)
		}
		selected, err := selectProjectToolboxManager(t.Context(), candidates, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxRepair})
		if selected != nil || !errors.Is(err, edgeclient.ErrProjectToolboxUnavailable) {
			t.Fatalf("selected=%T err=%v; repair must preserve uncertainty across endpoints", selected, err)
		}
		for _, manager := range managers {
			if manager.statusErr != edgeclient.ErrProjectToolboxContainerUnavailable && manager.repairCalls != 0 {
				t.Fatalf("missing/unavailable status triggered repair: status=%v repairs=%d", manager.statusErr, manager.repairCalls)
			}
		}
	}
}

func TestSelectProjectToolboxManagerDoesNotInferMissingWhenAnotherEndpointIsUnavailable(t *testing.T) {
	for _, managers := range [][]*fakeProjectToolboxManager{
		{{statusErr: edgeclient.ErrProjectToolboxContainerMissing}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}},
		{{statusErr: edgeclient.ErrProjectToolboxUnavailable}, {statusErr: edgeclient.ErrProjectToolboxContainerMissing}},
		{{statusErr: edgeclient.ErrProjectToolboxNotFound}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}},
		{{statusErr: edgeclient.ErrProjectToolboxUnavailable}, {statusErr: edgeclient.ErrProjectToolboxNotFound}},
	} {
		var candidates []projectToolboxOperations
		for _, manager := range managers {
			candidates = append(candidates, manager)
		}
		selected, err := selectProjectToolboxManager(t.Context(), candidates, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
		if selected != nil || !errors.Is(err, edgeclient.ErrProjectToolboxUnavailable) {
			t.Fatalf("selected=%T err=%v; unavailable endpoint must outrank another endpoint's empty inventory", selected, err)
		}
	}
}

func TestSelectProjectToolboxManagerIgnoresOtherEndpointAbsence(t *testing.T) {
	missing := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerMissing}
	otherEndpoint := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxEndpointStale}
	selected, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{missing, otherEndpoint}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
	if selected != nil || !errors.Is(err, edgeclient.ErrProjectToolboxContainerMissing) {
		t.Fatalf("selected=%T err=%v", selected, err)
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
	manager := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxContainerMissing}
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
	for _, test := range []struct {
		managers []*fakeProjectToolboxManager
		want     error
	}{
		{managers: []*fakeProjectToolboxManager{{statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}, {statusErr: edgeclient.ErrProjectToolboxContainerUnavailable}}, want: edgeclient.ErrProjectToolboxContainerUnavailable},
		{managers: []*fakeProjectToolboxManager{{statusErr: edgeclient.ErrProjectToolboxContainerMissing}, {statusErr: edgeclient.ErrProjectToolboxUnavailable}}, want: edgeclient.ErrProjectToolboxUnavailable},
		{managers: []*fakeProjectToolboxManager{{statusErr: edgeclient.ErrProjectToolboxUnavailable}, {statusErr: edgeclient.ErrProjectToolboxContainerMissing}}, want: edgeclient.ErrProjectToolboxUnavailable},
	} {
		var candidates []projectToolboxOperations
		for _, manager := range test.managers {
			candidates = append(candidates, manager)
		}
		selected, err := selectProjectToolboxManager(t.Context(), candidates, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxCleanup})
		if selected != nil || !errors.Is(err, test.want) {
			t.Fatalf("selected=%T err=%v; cleanup must not guess across endpoints", selected, err)
		}
		for _, manager := range test.managers {
			if manager.removed {
				t.Fatal("cleanup removed metadata while another endpoint was uncertain")
			}
		}
	}
}
