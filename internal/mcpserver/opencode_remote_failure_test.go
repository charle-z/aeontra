//go:build opencode_e2e

package mcpserver

import (
	"context"
	"errors"
	"os"
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

func TestRemoteOpenCodeE2EEmitsSafeStageMarkers(t *testing.T) {
	body, err := os.ReadFile("opencode_remote_e2e_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	stages := []string{
		"remote_stage_inputs",
		"remote_stage_authority",
		"remote_stage_workspace",
		"remote_stage_runtime",
		"remote_stage_edge_started",
		"remote_stage_first_turn",
		"remote_stage_processes",
	}
	last := -1
	for _, stage := range stages {
		index := strings.Index(text, "slice_code="+stage)
		if index < 0 || index <= last {
			t.Fatalf("missing or unordered safe stage %s index=%d last=%d", stage, index, last)
		}
		last = index
	}
}
