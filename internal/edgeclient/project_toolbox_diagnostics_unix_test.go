//go:build !windows

package edgeclient

import (
	"errors"
	"testing"
)

func TestProjectToolboxDiagnosticOwnershipErrorsRemainNotOwned(t *testing.T) {
	for _, err := range []error{
		ErrProjectToolboxContainerUnavailable,
		ErrProjectToolboxIdentityMismatch,
		ErrProjectToolboxMountMismatch,
		ErrProjectToolboxResourceMismatch,
		ErrProjectToolboxEnvironmentMismatch,
	} {
		if !errors.Is(err, ErrProjectToolboxNotOwned) {
			t.Fatalf("diagnostic error does not preserve not-owned semantics: %v", err)
		}
	}
}
