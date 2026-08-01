package main

import (
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func TestTopologyStartupCodeUsesClosedSanitizedStages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want startupCode
	}{
		{err: frontdoorcoordinator.ErrTopologyFrontApplication, want: startupTopologyFrontApplicationFailed},
		{err: frontdoorcoordinator.ErrTopologyBackendApplication, want: startupTopologyBackendApplicationFailed},
		{err: frontdoorcoordinator.ErrTopologyManagedIdentity, want: startupTopologyIdentityInvalid},
		{err: frontdoorcoordinator.ErrTopologyFrontBackend, want: startupTopologyFrontBackendFailed},
		{err: errors.New("raw upstream secret response"), want: startupTopologyValidationFailed},
	}
	for _, testCase := range cases {
		if got := topologyStartupCode(testCase.err); got != testCase.want {
			t.Fatalf("error=%v code=%s want=%s", testCase.err, got, testCase.want)
		}
	}
}
