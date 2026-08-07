//go:build !windows

package main

import (
	"testing"

	"github.com/charle-z/mcp-devbox/internal/edge"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestSafeProjectToolboxSelectionFailureCategories(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
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

func TestSelectProjectToolboxManagerRecoversAfterSpecificOwnershipMismatch(t *testing.T) {
	first := &fakeProjectToolboxManager{statusErr: edgeclient.ErrProjectToolboxMountMismatch}
	second := &fakeProjectToolboxManager{}
	got, err := selectProjectToolboxManager(t.Context(), []projectToolboxOperations{first, second}, toolboxSelectionFixture(), edge.Operation{Kind: edge.OperationProjectToolboxStatus})
	if err != nil || got != second {
		t.Fatalf("manager=%T err=%v", got, err)
	}
}
