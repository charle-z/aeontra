package main

import (
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/frontdoorcoordinator"
)

func TestTopologyStartupCodeClassifiesFrontApplicationRequestFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		detail error
		want   startupCode
	}{
		{detail: frontdoorcoordinator.ErrCoolifyRequestBuild, want: startupTopologyFrontApplicationBuild},
		{detail: frontdoorcoordinator.ErrCoolifyRequestTransport, want: startupTopologyFrontApplicationTransport},
		{detail: frontdoorcoordinator.ErrCoolifyResponseRead, want: startupTopologyFrontApplicationRead},
		{detail: frontdoorcoordinator.ErrCoolifyResponseHTTP, want: startupTopologyFrontApplicationHTTP},
		{detail: frontdoorcoordinator.ErrCoolifyResponseDecode, want: startupTopologyFrontApplicationDecode},
		{detail: frontdoorcoordinator.ErrCoolifyIdentity, want: startupTopologyFrontApplicationIdentity},
	}
	for _, testCase := range cases {
		err := errors.Join(frontdoorcoordinator.ErrTopologyFrontApplication, testCase.detail)
		if got := topologyStartupCode(err); got != testCase.want {
			t.Fatalf("detail=%v code=%s want=%s", testCase.detail, got, testCase.want)
		}
	}
}
