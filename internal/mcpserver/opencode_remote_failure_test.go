//go:build opencode_e2e

package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSafeRemoteEdgeFailureCodeIsClosedAndRedacted(t *testing.T) {
	cases := []struct {
		err    error
		stderr string
		want   string
	}{
		{errors.New("exit status 1 /private/path"), "mcp-edge: OpenCode runtime failed safely runtime=mr_secret state=failed failure=opencode_driver_status prompt payload body", "opencode_driver_status"},
		{context.DeadlineExceeded, "secret SQL SELECT", "timeout"},
		{context.Canceled, "secret", "cancelled"},
		{errors.New("exit status 1"), "failure=attacker_payload", "internal"},
		{errors.New("exit status 1"), "", "process_exit"},
		{nil, "", "internal"},
	}
	for _, test := range cases {
		got := safeRemoteEdgeFailureCode(test.err, test.stderr)
		if got != test.want {
			t.Fatalf("code=%q want=%q", got, test.want)
		}
		for _, forbidden := range []string{"private", "secret", "prompt", "payload", "body", "SELECT", "runtime="} {
			if strings.Contains(got, forbidden) {
				t.Fatalf("safe code leaked %q: %q", forbidden, got)
			}
		}
	}
}
