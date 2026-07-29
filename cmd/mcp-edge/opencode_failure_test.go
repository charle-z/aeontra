//go:build !windows

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/charle-z/mcp-devbox/internal/bundle"
	"github.com/charle-z/mcp-devbox/internal/edgeclient"
)

func TestOpenCodeFailureCodeIsStableAndRedacted(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, "none"},
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "cancelled"},
		{edgeclient.ErrKillSwitch, "kill_switch"},
		{edgeclient.ErrOpenCodeInterrupted, "restart_interrupted"},
		{edgeclient.ErrOpenCodeTerminal, "terminal_replay"},
		{edgeclient.ErrEdgeInstanceLocked, "instance_lock_occupied"},
		{errors.New("OpenCode integrity does not match the pinned release"), "installation_integrity"},
		{errors.New("OpenCode version does not match the pinned release"), "installation_version"},
		{errors.New("pinned OpenCode executable is unsafe"), "installation_opencode"},
		{errors.New("OpenCode external driver manifest is invalid"), "installation_provider"},
		{errors.New("model-turn driver executable is unsafe"), "installation_driver"},
		{errors.New("bubblewrap verification failed (bubblewrap_netlink_route_denied)"), "bubblewrap_netlink_route_denied"},
		{errors.New("OpenCode model-turn socket is not private"), "socket"},
		{errors.New("OpenCode terminated unexpectedly (request_stage)"), "opencode_request_stage"},
		{errors.New("OpenCode terminated unexpectedly (turn_create)"), "opencode_turn_create"},
		{errors.New("OpenCode terminated unexpectedly (response_wait)"), "opencode_response_wait"},
		{errors.New("OpenCode terminated unexpectedly"), "opencode_exit"},
		{&bundle.VerificationError{Code: bundle.BundleMismatch}, "bundle_mismatch"},
		{&bundle.VerificationError{Code: bundle.ProviderOutdated}, "provider_outdated"},
		{&bundle.VerificationError{Code: bundle.DriverOutdated}, "driver_outdated"},
		{&bundle.VerificationError{Code: bundle.ManifestInvalid}, "manifest_invalid"},
		{errors.New("unknown sensitive detail /private/path"), "internal"},
	}
	for _, test := range cases {
		if got := openCodeFailureCode(test.err); got != test.want {
			t.Fatalf("error=%v code=%q want=%q", test.err, got, test.want)
		}
	}
}
