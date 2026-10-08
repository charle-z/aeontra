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
		"remote_stage_payload",
		"remote_stage_results",
		"remote_stage_response",
		"remote_stage_responded",
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

func TestAccumulateDirectoryBytesIgnoresOnlyVanishedDescendants(t *testing.T) {
	root := "/evidence"
	var total int64
	for _, suffix := range []string{"browser.db-wal", "browser.db-shm"} {
		vanished := &os.PathError{Op: "lstat", Path: root + "/project-browser/" + suffix, Err: os.ErrNotExist}
		if err := stableDirectoryWalkError(root, vanished.Path, vanished); err != nil {
			t.Fatalf("vanished %s should be ignored by shared walker policy: %v", suffix, err)
		}
		if err := accumulateDirectoryBytes(root, vanished.Path, nil, vanished, &total); err != nil {
			t.Fatalf("vanished %s should be ignored by byte inventory: %v", suffix, err)
		}
	}
	if total != 0 {
		t.Fatalf("vanished sidecar changed total: %d", total)
	}
	missingRoot := &os.PathError{Op: "lstat", Path: root, Err: os.ErrNotExist}
	if err := accumulateDirectoryBytes(root, root, nil, missingRoot, &total); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing root error=%v", err)
	}
	permission := &os.PathError{Op: "lstat", Path: root + "/blocked", Err: os.ErrPermission}
	if err := accumulateDirectoryBytes(root, permission.Path, nil, permission, &total); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("permission error=%v", err)
	}
}
