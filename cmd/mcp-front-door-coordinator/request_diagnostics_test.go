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
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateTarget), want: startupCode("topology_front_application_transport_target_failed")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateResolve), want: startupCode("topology_front_application_transport_resolution_failed")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateAddress), want: startupCode("topology_front_application_transport_address_policy_failed")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateRefused), want: startupCode("topology_front_application_transport_connection_refused")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateTimeout), want: startupCode("topology_front_application_transport_connection_timed_out")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateRoute), want: startupCode("topology_front_application_transport_route_unavailable")},
		{detail: errors.Join(frontdoorcoordinator.ErrCoolifyRequestTransport, frontdoorcoordinator.ErrCoolifyPrivateConnect), want: startupCode("topology_front_application_transport_connection_failed")},
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
